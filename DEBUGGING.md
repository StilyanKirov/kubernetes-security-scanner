# Debug на ImageScan оператора в Kind с VS Code

Проверено спрямо проекта и работещия Deployment на 25 октомври 2026 г.
Всички shell команди по-долу са за **WSL Bash**, от папката на проекта.
Това ръководство предполага вече инсталирани оператор, CRD, cert-manager,
webhook сертификати и report PV/PVC в съществуващия локален Kind клъстер.

## Step 1: Разбери кои настройки запазихме

| Настройка | Нормален dev режим | Финален debug режим | Причина |
|---|---|---|---|
| Image | `imagescan-operator:dev` | `imagescan-operator:debug` | Debug образът съдържа Delve |
| Container command | `/manager` | Премахната | Позволява image ENTRYPOINT да стартира Delve |
| Memory limit | `128Mi` | `512Mi` | При 128Mi наблюдавахме OOMKilled |
| Liveness probe | `/healthz` | Премахната | Паузата в debugger спира отговора; иначе kubelet рестартира контейнера |
| Readiness probe | `/readyz` | Премахната | Иначе паузираният Pod изчезва от готовите Service endpoints |
| Leader election | `--leader-elect` | Аргументът е премахнат | При пауза lease не се подновява и manager приключва |
| Webhook timeout | `10s` | `10s` | Остава ограничение при breakpoint във webhook-а |

CPU limit остава `500m`; requests остават CPU `10m`, memory `64Mi`.
Security context не е променян за debugging: non-root, RuntimeDefault seccomp,
drop ALL capabilities, readOnlyRootFilesystem и без privilege escalation.
Не сме добавяли SYS_PTRACE или privileged режим.

Първо увеличихме probe failureThreshold на 100, но после премахнахме самите
probes. Не е нужно да повтаряш тези междинни настройки.

Debug настройките са в **живия Deployment в Kind**. Провереният
`config/manager/manager.yaml` все още съдържа нормалните probes, `/manager`,
`--leader-elect` и `128Mi`. Повторно deploy-ване може да промени debug настройките.

## Step 2: Провери инструментите

Отвори проекта във VS Code през WSL. Go разширението трябва да е активирано
в WSL. Проверката по-долу само показва версията на локалния Delve:

```bash
dlv version
```

При липсващ Delve: VS Code Command Palette → `Go: Install/Update Tools` → `dlv`.
При нашата сесия версията беше 1.27.2.

## Step 3: Построй debug образа

В проекта вече има `Dockerfile.debug`:

```dockerfile
FROM golang:1.26

WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
RUN go install github.com/go-delve/delve/cmd/dlv@v1.27.2

COPY . .
RUN CGO_ENABLED=0 go build -gcflags="all=-N -l" -o /manager ./cmd

USER 65532:65532
ENTRYPOINT ["/go/bin/dlv", "exec", "--headless", "--listen=:40000", "--accept-multiclient", "--continue", "/manager", "--"]
```

`-N -l` изключва compiler optimizations и inlining за debugging.
`--continue` стартира manager веднага; `--accept-multiclient` позволява повторно
свързване. Последното `--` отделя аргументите за manager от тези за Delve.

Следващата команда създава локален Docker образ; не променя клъстера:

```bash
docker build -f Dockerfile.debug -t imagescan-operator:debug .
```

Повтаряй build след промени по Go кода. Кодът във VS Code трябва да съвпада
с компилирания код в образа, за да са точни breakpoint-ите и стойностите.

## Step 4: Зареди образа в Kind

Командата копира образа в image cache на Kind node-а. Не създава Deployment:

```bash
kind load docker-image imagescan-operator:debug --name kind
```

`kind` е името на клъстера; `kind-kind` е неговият kubectl context.

## Step 5: Смени dev с debug и приложи всички временни настройки

Следващата команда променя Deployment-а в Kind и предизвиква подмяна на Pod-а,
ако конфигурацията се е променила. Не редактира файловете в проекта.
Използва strategic merge по името на контейнера, вместо позиции в списъка.
Подходяща е за нашия локален Deployment с една реплика.

```bash
kubectl --context kind-kind patch deployment imagescan-project-controller-manager \
  -n imagescan-project-system \
  --type=strategic \
  -p='{
    "spec": {
      "template": {
        "spec": {
          "containers": [{
            "name": "manager",
            "image": "imagescan-operator:debug",
            "imagePullPolicy": "IfNotPresent",
            "command": null,
            "args": [
              "--metrics-bind-address=:8443",
              "--health-probe-bind-address=:8081",
              "--webhook-cert-path=/tmp/k8s-webhook-server/serving-certs",
              "--webhook-port=9443"
            ],
            "livenessProbe": null,
            "readinessProbe": null,
            "resources": {
              "limits": {"cpu": "500m", "memory": "512Mi"},
              "requests": {"cpu": "10m", "memory": "64Mi"}
            }
          }]
        }
      }
    }
  }'
```

Само `kubectl set image` не е достатъчно: старата `command: ["/manager"]`
би заобиколила Delve ENTRYPOINT.

Ако Deployment-ът вече е в debug режим и си заредил нов образ със същия tag,
patch-ът може да не промени Pod template. Тогава тази команда подменя Pod-а,
за да стартира от заредения образ:

```bash
kubectl --context kind-kind rollout restart deployment/imagescan-project-controller-manager \
  -n imagescan-project-system
```

## Step 6: Провери стартирането

Тази команда само изчаква rollout-а:

```bash
kubectl --context kind-kind rollout status deployment/imagescan-project-controller-manager \
  -n imagescan-project-system --timeout=120s
```

Без readiness probe успешният rollout **не доказва**, че manager и webhook
вече слушат. Прочети логовете:

```bash
kubectl --context kind-kind logs \
  -n imagescan-project-system \
  deployment/imagescan-project-controller-manager \
  -c manager --tail=60
```

Търси `API server listening at: [::]:40000` (Delve),
`Serving webhook server` и `Starting workers` (manager).

## Step 7: Отвори port-forward

В отделен WSL терминал изпълни следната команда. Тя създава локална връзка
към Delve в Pod-а; не променя Deployment-а:

```bash
kubectl --context kind-kind port-forward \
  -n imagescan-project-system \
  deployment/imagescan-project-controller-manager \
  40000:40000 --address 127.0.0.1
```

Остави терминала отворен. Очакваш `Forwarding from 127.0.0.1:40000`.
`Handling connection` означава връзка към порта, но не доказва успешно attach.
При подмяна на Pod-а спри стария port-forward с Ctrl+C и го стартирай отново.
Не създаваме Service за debug порта. Delve няма authentication в тази настройка;
използвай я само в този локален тестов клъстер.

## Step 8: Прикачи VS Code

В проекта вече има `.vscode/launch.json`:

```json
{
  "version": "0.2.0",
  "configurations": [
    {
      "name": "Attach to ImageScan in Kind",
      "type": "go",
      "request": "attach",
      "mode": "remote",
      "debugAdapter": "dlv-dap",
      "host": "127.0.0.1",
      "port": 40000,
      "substitutePath": [
        {"from": "${workspaceFolder}", "to": "/workspace"}
      ]
    }
  ]
}
```

`substitutePath` свързва локалните файлове с build пътя `/workspace` в образа.
В Run and Debug избери `Attach to ImageScan in Kind` и натисни F5.
Друг начин: Ctrl+Shift+P → `Debug: Select and Start Debugging`.
Ако конфигурацията вече работи, не стартирай второ копие.

## Step 9: Постави breakpoint и подготви теста

За спокойно разглеждане на контролера постави breakpoint на първия изпълним
ред на `Reconcile` в `internal/controller/imagescan_controller.go`
(`scan := &scanv1.ImageScan{}`, ред 60 при проверката).
Изключи webhook breakpoint-ите за този тест.

Следващите команди само проверяват тестовите ресурси и PVC:

```bash
kubectl --context kind-kind get namespace imagescan-test --show-labels
kubectl --context kind-kind get pods -n imagescan-test -l app=web
kubectl --context kind-kind get pvc imagescan-results-pvc -n imagescan-project-system
```

Очакваме namespace `imagescan-test` с label `env=production`, Pod `web-test`
с label `app=web` и PVC `Bound`. Селекторите търсят **labels**, не имена.

Ако тестовият namespace или Pod липсва, тези команди ги създават/обновяват в Kind:

```bash
kubectl --context kind-kind apply -f config/samples/imagescan-test-namespace.yaml
kubectl --context kind-kind apply -f config/samples/imagescan-test-pod.yaml
```

Тестовият `config/samples/imagescan-debug.yaml` съдържа:

```yaml
apiVersion: scan.my.domain/v1
kind: ImageScan
metadata:
  name: imagescan-debug
spec:
  namespaceSelector:
    matchLabels:
      env: production
  podSelector:
    matchLabels:
      app: web
```

## Step 10: Създай или повтори scan-а

За първото създаване командата записва ImageScan в Kind и задейства webhook-а:

```bash
kubectl --context kind-kind apply -f config/samples/imagescan-debug.yaml
```

За ново сканиране със същия YAML първо изтрий стария тестов ImageScan, после го
създай отново. Това изтрива стария му status и може да премахне Jobs, които той
притежава. Запазените JSON файлове в PVC не се изтриват от тези команди.

```bash
kubectl --context kind-kind delete -f config/samples/imagescan-debug.yaml --ignore-not-found
kubectl --context kind-kind apply -f config/samples/imagescan-debug.yaml
```

Новият ресурс има нов UID и съответно нови scan IDs. Повторен `apply` върху
съществуващ завършен ImageScan не започва ново сканиране в текущата имплементация.

## Step 11: Разглеждай изпълнението

- F10: изпълни текущия ред, без да влизаш във функцията.
- F11: влез във функцията.
- F5: продължи до следващ breakpoint.
- Variables / Watch / Debug Console: разглеждай `scan`, `req`, `err` и други
  променливи, които са в обхвата на избрания stack frame.

`Reconcile` се извиква многократно: наблюдаваме ImageScan и притежаваните Jobs.
Status updates, промени по Job и изтриването му могат да причинят нови извиквания.
Няма фиксиран брой извиквания или зададен `RequeueAfter` в текущия код.

### Ограничение при debug на webhook

Breakpoint във `ValidateCreate` спира заявка с **10 секунди timeout**.
Ако разглеждаш по-дълго, Kubernetes прекратява заявката и `ctx` се отменя.
Потвърдихме, че `v.Reader.List(ctx, ...)` тогава връща `context canceled`.
Премахването на probes и leader election не отменя този timeout.

За да видиш webhook → controller в една заявка, продължи бързо от webhook-а.
За дълго разглеждане използвай breakpoint в контролера. След отменен webhook
контекст трябва нова заявка; Continue не възстановява стария контекст.

## Step 12: Следи логовете и резултата

Следващата команда само следи manager логовете в отделен терминал:

```bash
kubectl --context kind-kind logs \
  -n imagescan-project-system \
  deployment/imagescan-project-controller-manager -c manager -f
```

Тук се виждат `logf.FromContext(ctx).Info(...)` съобщенията. Ако breakpoint е
на самия logging ред, изпълни го с F10, преди да очакваш съобщението.
Върната от Reconcile грешка се логва от controller-runtime; самото
`return fmt.Errorf(...)` не е команда за отпечатване.

Тези команди само показват status-а и файловете в PVC:

```bash
kubectl --context kind-kind get imagescan imagescan-debug -o yaml
kubectl --context kind-kind exec -n imagescan-project-system \
  deployment/imagescan-project-controller-manager -c manager -- ls -lh /reports
```

За отваряне на JSON във VS Code замени `REPORT_ID.json` с реалното име.
Следващата команда копира съдържанието в WSL `/tmp`; презаписва локалното копие,
но не променя файла в PVC:

```bash
kubectl --context kind-kind exec -n imagescan-project-system \
  deployment/imagescan-project-controller-manager -c manager \
  -- cat /reports/REPORT_ID.json > /tmp/imagescan-report.json
```

Тази команда отваря локалното копие в редактора:

```bash
code /tmp/imagescan-report.json
```

`ls` и `cat` са налични в debug образа; не разчитай на тях в нормалния dev образ.

## Step 13: Възстанови прекъсната debug сесия

| Симптом | Проверена причина или следваща проверка |
|---|---|
| `OOMKilled` | При 128Mi паметта не стигаше; финалният лимит е 512Mi |
| `Liveness probe failed` | Kubelet рестартира паузиран процес; в debug режима probes трябва да липсват |
| Празни Service endpoints | Readiness може да извади паузирания Pod; финалният debug режим няма тази probe |
| `leader election lost` | Проверка дали `--leader-elect` отново е добавен |
| `context canceled` във webhook | Заявката е отменена; при нашия тест беше след дълга debug пауза |
| `process ... has exited` при attach | Manager процесът е приключил; Delve може още да държи контейнера работещ |
| `connection refused` към webhook | Провери дали manager слуша и Service има endpoint; Running Pod сам по себе си не доказва това |

При приключил manager следващата команда създава нов Pod:

```bash
kubectl --context kind-kind rollout restart deployment/imagescan-project-controller-manager \
  -n imagescan-project-system
```

Повтори Step 6 (логове), Step 7 (нов port-forward) и Step 8 (нов attach).
При напускане на сесията предпочитай **Disconnect** и остави процеса да работи.
**Stop** може да приключи manager-а. Не оставяй процеса паузиран при disconnect.

За диагностика следващите команди само четат Pod status и Service endpoints:

```bash
kubectl --context kind-kind get pods -n imagescan-project-system -l control-plane=controller-manager
kubectl --context kind-kind get endpointslices -n imagescan-project-system \
  -l kubernetes.io/service-name=imagescan-project-webhook-service
```

## Step 14: Върни нормалния dev режим

Приключи debug сесията и спри port-forward с Ctrl+C.
Преди връщане използвай актуален dev образ. Build създава локален образ,
а load го копира в Kind:

```bash
make docker-build IMG=imagescan-operator:dev
kind load docker-image imagescan-operator:dev --name kind
```

Следният patch възстановява проверените нормални настройки в Deployment-а:
dev image, `/manager`, leader election, двете probes и memory limit 128Mi.
Той променя клъстера и подменя debug Pod-а.

```bash
kubectl --context kind-kind patch deployment imagescan-project-controller-manager \
  -n imagescan-project-system \
  --type=strategic \
  -p='{
    "spec": {
      "template": {
        "spec": {
          "containers": [{
            "name": "manager",
            "image": "imagescan-operator:dev",
            "command": ["/manager"],
            "args": [
              "--metrics-bind-address=:8443",
              "--leader-elect",
              "--health-probe-bind-address=:8081",
              "--webhook-cert-path=/tmp/k8s-webhook-server/serving-certs",
              "--webhook-port=9443"
            ],
            "livenessProbe": {
              "httpGet": {"path": "/healthz", "port": 8081},
              "initialDelaySeconds": 15,
              "periodSeconds": 20,
              "failureThreshold": 3
            },
            "readinessProbe": {
              "httpGet": {"path": "/readyz", "port": 8081},
              "initialDelaySeconds": 5,
              "periodSeconds": 10,
              "failureThreshold": 3
            },
            "resources": {
              "limits": {"cpu": "500m", "memory": "128Mi"},
              "requests": {"cpu": "10m", "memory": "64Mi"}
            }
          }]
        }
      }
    }
  }'
```

Провери възстановяването с командата за rollout status от Step 6 и прочети
логовете. Запазването на това ръководство не изпълнява възстановяването.

Общите patch команди тук обединяват проверените настройки от сесията.
Не са приложени повторно към клъстера при създаване на документа.
