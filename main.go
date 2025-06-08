package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	acme "github.com/cert-manager/cert-manager/pkg/acme/webhook/apis/acme/v1alpha1"
	"github.com/cert-manager/cert-manager/pkg/acme/webhook/cmd"

	corev1 "k8s.io/api/core/v1"
	extapi "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const dnsAPIBaseURL = "https://mcs.mail.ru/public-dns/v2/dns/"

var GroupName = os.Getenv("GROUP_NAME")

var (
	// These flags are required by cert-manager for TLS
	tlsCertFile       = flag.String("tls-cert-file", "", "Path to the TLS certificate file")
	tlsPrivateKeyFile = flag.String("tls-private-key-file", "", "Path to the TLS private key file")
)

func main() {
	flag.Parse()

	if GroupName == "" {
		panic("GROUP_NAME must be specified")
	}

	cmd.RunWebhookServer(GroupName, &vkcloudDNSSolver{})
}

// vkcloudDNSSolver implements the cert-manager Solver interface for VK Cloud DNS
type vkcloudDNSSolver struct {
	client *kubernetes.Clientset
}

// vkcloudDNSConfig is passed by cert-manager during challenge
type vkcloudDNSConfig struct {
	SecretRef corev1.SecretReference `json:"secretRef"`
	Domain    string                 `json:"domain,omitempty"`
}

// Name returns solver name
func (c *vkcloudDNSSolver) Name() string {
	return "cert-manager-webhook-vkcloud"
}

// Present creates a TXT record for ACME challenge
func (c *vkcloudDNSSolver) Present(ch *acme.ChallengeRequest) error {
	cfg, err := loadConfig(ch.Config)
	if err != nil {
		return err
	}

	authToken, err := authenticate(c.client, ch.ResourceNamespace, cfg.SecretRef)
	if err != nil {
		return fmt.Errorf("failed to authenticate to VK Cloud: %w", err)
	}

	dnsZoneID, err := getZoneID(ch.ResolvedZone, authToken)
	if err != nil {
		return fmt.Errorf("failed to find zone ID: %w", err)
	}

	err = createTXTRecord(dnsZoneID, ch.ResolvedFQDN, ch.Key, authToken)
	if err != nil {
		return fmt.Errorf("failed to create TXT record: %w", err)
	}

	return nil
}

// CleanUp deletes the TXT record after validation
func (c *vkcloudDNSSolver) CleanUp(ch *acme.ChallengeRequest) error {
	cfg, _ := loadConfig(ch.Config)
	authToken, err := authenticate(c.client, ch.ResourceNamespace, cfg.SecretRef)
	if err != nil {
		return err
	}

	dnsZoneID, err := getZoneID(ch.ResolvedZone, authToken)
	if err != nil {
		return err
	}

	err = deleteTXTRecord(dnsZoneID, ch.ResolvedFQDN, ch.Key, authToken)
	if err != nil {
		return err
	}

	return nil
}

// Initialize sets up Kubernetes client
func (c *vkcloudDNSSolver) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	cl, err := kubernetes.NewForConfig(kubeClientConfig)
	if err != nil {
		return err
	}
	c.client = cl
	return nil
}

// loadConfig decodes JSON configuration into our struct
func loadConfig(cfgJSON *extapi.JSON) (vkcloudDNSConfig, error) {
	cfg := vkcloudDNSConfig{}
	if cfgJSON == nil {
		return cfg, nil
	}
	if err := json.Unmarshal(cfgJSON.Raw, &cfg); err != nil {
		return cfg, fmt.Errorf("error decoding solver config: %w", err)
	}
	return cfg, nil
}

// authenticate gets X-Subject-Token
func authenticate(clientset *kubernetes.Clientset, namespace string, secretRef corev1.SecretReference) (string, error) {
	secret, err := clientset.CoreV1().Secrets(namespace).Get(context.TODO(), secretRef.Name, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to fetch secret %q: %w", secretRef.Name, err)
	}

	osAuthURL := string(secret.Data["os_auth_url"])
	osUsername := string(secret.Data["os_username"])
	osPassword := string(secret.Data["os_password"])
	osProjectID := string(secret.Data["os_project_id"])
	osDomainName := string(secret.Data["os_domain_name"])

	body := map[string]any{
		"auth": map[string]any{
			"identity": map[string]any{
				"methods": []string{"password"},
				"password": map[string]any{
					"user": map[string]any{
						"name":     osUsername,
						"password": osPassword,
						"domain": map[string]string{
							"id": osDomainName,
						},
					},
				},
			},
			"scope": map[string]any{
				"project": map[string]string{
					"id": osProjectID,
				},
			},
		},
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}

	req, _ := http.NewRequest("POST", osAuthURL, bytes.NewBuffer(jsonBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("authentication failed: %w", err)
	}
	defer resp.Body.Close()

	token := resp.Header.Get("X-Subject-Token")
	if token == "" {
		return "", fmt.Errorf("no auth token received from VK Cloud")
	}

	return token, nil
}

// getZoneID finds zone UUID by domain name
func getZoneID(zone string, token string) (string, error) {
	url := dnsAPIBaseURL

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-Auth-Token", token)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch zones: %w", err)
	}
	defer resp.Body.Close()

	var zones []struct {
		UUID string `json:"uuid"`
		Name string `json:"zone"`
	}

	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &zones); err != nil {
		return "", fmt.Errorf("failed to parse zones response: %w", err)
	}

	for _, z := range zones {
		if strings.TrimSuffix(z.Name, ".") == strings.TrimSuffix(zone, ".") {
			return z.UUID, nil
		}
	}

	return "", fmt.Errorf("zone not found for domain %s", zone)
}

// createTXTRecord adds a TXT record under the zone
func createTXTRecord(zoneID, fqdn, key, token string) error {
	record := map[string]any{
		"name":    fqdn,
		"content": key,
		"ttl":     60,
	}

	zoneTXTRecordsURL := dnsAPIBaseURL + "%s/txt/"
	url := fmt.Sprintf(zoneTXTRecordsURL, zoneID)
	jsonBody, _ := json.Marshal(record)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}
	req.Header.Set("X-Auth-Token", token)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to create TXT record: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("create failed with status %d: %s", resp.StatusCode, body)
	}

	return nil
}

// deleteTXTRecord removes the specific TXT record
func deleteTXTRecord(zoneID, fqdn, key, token string) error {
	zoneTXTRecordsURL := dnsAPIBaseURL + "%s/txt/"
	url := fmt.Sprintf(zoneTXTRecordsURL, zoneID)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-Auth-Token", token)
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var recordsResponse struct {
		TXTRecords []struct {
			UUID    string `json:"uuid"`
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"txt_records"`
	}

	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &recordsResponse); err != nil {
		return fmt.Errorf("failed to parse TXT records: %w", err)
	}

	for _, r := range recordsResponse.TXTRecords {
		if r.Name == fqdn && r.Content == key {
			zoneTXTRecordsURL := dnsAPIBaseURL + "%s/txt/%s"
			deleteURL := fmt.Sprintf(zoneTXTRecordsURL, zoneID, r.UUID)

			delReq, _ := http.NewRequest("DELETE", deleteURL, nil)
			delReq.Header.Set("X-Auth-Token", token)

			resp, err := httpClient.Do(delReq)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			break
		}
	}

	return nil
}
