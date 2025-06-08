# VK Cloud DNS ACME webhook

## Usage
### Setup Kubernetes
You can use any Kubernetes service\
[Install cert-manager](https://cert-manager.io/docs/installation/) \
[Install helm](https://v2.helm.sh/docs/using_helm/#installing-helm)

### Install webhook
```shell
git clone https://github.com/drone076/cert-manager-webhook-vkcloud.git
```

```shell
helm install -n cert-manager vkcloud-webhook ./deploy/cert-manager-webhook-vkcloud
```

