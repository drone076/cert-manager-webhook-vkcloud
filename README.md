 # VK Cloud DNS ACME webhook
 
 **cert-manager-webhook-vkcloud** — это [webhook](https://cert-manager.io/docs/configuration/acme/dns01/#webhook) для [cert-manager](https://cert-manager.io), предназначенный для автоматической проверки доменов через DNS REST API **VK Cloud**. 

 Этот webhook позволяет выписывать TLS-сертификаты через Let's Encrypt (или другой ACME провайдер), используя DNS-01 challenge и автоматически создавая TXT-записи в зоне VK Cloud DNS.

 ## Почему standalone?

 В отличие от большинства решений, которые требуют зависимости от [`jetstack/cert-manager`](https://github.com/jetstack/cert-manager) и настройки авторизации, этот webhook может быть установлен **без зависимости от jetstack**, как самостоятельный компонент. Это удобно, если:

 - Вы хотите минимизировать количество зависимостей.
 - У вас уже установлен cert-manager и вы просто добавляете новый DNS-провайдер.
 - Вы работаете в частной Kubernetes-среде и предпочитаете минимальные инсталляции.

 ## Возможности

 - Поддержка DNS-01 challenge через REST API VK Cloud
 - Простая интеграция с cert-manager
 - Поддержка нескольких доменов
 - Совместимость с Let's Encrypt и другими ACME-провайдерами

 ---

 ## Требования

 - Kubernetes 1.16+
 - cert-manager v1.6+ (установлен отдельно)
 - Доступ к VK Cloud (логин, пароль, ID проекта, домен)

 ---

 ## Установка

 ### 1. Установите cert-manager (если еще не установлен)

 Инструкция:  
 [https://cert-manager.io/docs/installation/](https://cert-manager.io/docs/installation/) 

 ### 2. Клонируйте репозиторий

 ```bash
 git clone https://github.com/drone076/cert-manager-webhook-vkcloud.git 
 cd cert-manager-webhook-vkcloud
 ```

 ### 3. Установите webhook с помощью Helm

 ```bash
 helm install -n cert-manager-webhook-vkcloud ./deploy/cert-manager-webhook-vkcloud --namespace cert-manager -f ./deploy/cert-manager-webhook-vkcloud/values.yaml
 ```

 > Обратите внимание: webhook будет запущен как Deployment и зарегистрирован в cert-manager по имени `groupName`, указанному в коде (например: `acme.cloud.vk.com`).

### 4. Удаление с помощью Helm

```bash
helm uninstall -n cert-manager cert-manager-webhook-vkcloud
```

---

 ## Настройка

 ### 1. Создайте секрет с учетными данными VK Cloud

 Сохраните файл `vkcloud-secret.yaml`:

 ```yaml
 apiVersion: v1
 kind: Secret
 metadata:
   name: vkcloud-secret
   namespace: cert-manager
 type: Opaque
 stringData:
   os_auth_url: "https://infra.mail.ru:35357/v3/auth/tokens" 
   os_username: "<ваш_логин>"
   os_password: "<ваш_пароль>"
   os_project_id: "<ID_проекта>"
   os_domain_name: "users"
 ```

 Примените его:

 ```bash
 kubectl apply -f vkcloud-secret.yaml
 ```

 ### 2. Пример ClusterIssuer

 Создайте `clusterissuer.yaml`:

 ```yaml
 apiVersion: cert-manager.io/v1
 kind: ClusterIssuer
 metadata:
   name: sampleclusterissuer
 spec:
   acme:
     email: your@email.com
     privateKeySecretRef:
       name: letsencrypt
     server: https://acme-v02.api.letsencrypt.org/directory 
     solvers:
       - dns01:
           webhook:
             groupName: acme.cloud.vk.com
             solverName: cert-manager-webhook-vkcloud
             config:
               secretRef:
                 name: vkcloud-secret
                 namespace: cert-manager
        # Добавьте по желанию выбор доменов в этом блоке, если его нет, солвер будет использован по-умолчанию
        selector:
          dnsNames:
          - example.com
          ...
 ```

 Примените:

 ```bash
 kubectl apply -f clusterissuer.yaml
 ```

 ### 3. Пример Certificate

 ```yaml
 apiVersion: cert-manager.io/v1
 kind: Certificate
 metadata:
   name: example.com-tls
   namespace: default
 spec:
   secretName: example.com-tls
   issuerRef:
     name: sampleclusterissuer
     kind: ClusterIssuer
   dnsNames:
     - example.com
 ```

 ---

 ## Как это работает

 1. cert-manager вызывает webhook, указанный в `ClusterIssuer`.
 2. Webhook аутентифицируется в VK Cloud через API.
 3. Получает список DNS-зон и находит нужную.
 4. Создает временную TXT-запись для подтверждения владения доменом.
 5. После успешной проверки, сертификат выдается.
 6. TXT-запись удаляется после завершения проверки.

 ---

 ## Отладка

 Смотрите clusterissuer, orders, challenge, cert для соответствующих namespace, например в k9s.

 ## Поддерживаемые версии cert-manager

 - ✅ cert-manager v1.6+
 - ✅ cert-manager v1.17.2 (рекомендуемая)

 ---

 ## Примеры конфигураций

 ### `vkcloud-secret.yaml`

 ```yaml
 apiVersion: v1
 kind: Secret
 metadata:
   name: vkcloud-secret
   namespace: cert-manager
 type: Opaque
 stringData:
   os_auth_url: "https://infra.mail.ru:35357/v3/auth/tokens" 
   os_username: "<ваш_логин>"
   os_password: "<ваш_пароль>"
   os_project_id: "<ID_проекта>"
   os_domain_name: "users"
 ```

 ### `clusterissuer.yaml`

 ```yaml
 apiVersion: cert-manager.io/v1
 kind: ClusterIssuer
 metadata:
   name: sampleclusterissuer
 spec:
   acme:
     email: your@email.com
     privateKeySecretRef:
       name: letsencrypt
     server: https://acme-v02.api.letsencrypt.org/directory 
     solvers:
       - dns01:
           webhook:
             groupName: acme.cloud.vk.com
             solverName: cert-manager-webhook-vkcloud
             config:
               secretRef:
                 name: vkcloud-secret
                 namespace: cert-manager
 ```

 ---

 ## FAQ

 ### Почему нет зависимости от Jetstack?

 Webhook написан так, чтобы работать автономно — он сам реализует необходимый интерфейс для cert-manager, и вам не нужно устанавливать дополнительных CRD или контроллеров. Использование внешнего хука сильно усложняет настройку RBAC, или даже вообще делает ее невозможной вызывая ошибки с ***system:anonymous***

 ### Нужен ли Docker?

 Да, но образ уже собран и доступен на Docker Hub / GitLab. Вы также можете собрать свой, если потребуется кастомизация.

 ### Что делать, если домен не находится?

 Убедитесь, что у пользователя в VK Cloud есть права на чтение и изменение DNS-зон, и что домен добавлен в аккаунте. Смотрите ошибки в challenge.

 ---

 ## Лицензия

GNU General Public License v3.0 — смотрите [LICENSE](LICENSE)

 ---

 ## Автор

 [drone076](https://github.com/drone076) 

 ---

 ## Ссылки

 - [VK Cloud Public DNS API](https://cloud.vk.com/docs/ru/networks/dns/publicdns) 
 - [cert-manager docs](https://cert-manager.io/docs/) 
 - [Helm docs](https://v2.helm.sh/docs/using_helm/) 

 - [Имидж на DockerHub](https://hub.docker.com/r/drone076/cert-manager-webhook-vkcloud)