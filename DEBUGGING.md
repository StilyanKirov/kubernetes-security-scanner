# Debug на ImageScan оператора в Kind с VS Code

Първоначална debug сесия с работещия Deployment: 25 октомври 2026 г.
Актуализирано спрямо локалния код и manifests на 27 октомври 2026 г.;
новите HTTP стъпки и тестови manifests не са изпълнявани в клъстера при тази редакция.
Всички shell команди по-долу са за **WSL Bash**, от папката на проекта.
Това ръководство предполага вече инсталирани оператор, CRD, cert-manager,
webhook сертификати и report PV/PVC в съществуващия локален Kind клъстер.

## Бърз старт: dev или debug с една команда

Targets са за вече инсталирания оператор с една реплика в локалния Kind клъстер
`kind`. Използват текущия kubectl context, който трябва да сочи към този клъстер.
Не превключват context и не използват e2e клъстера от `KIND_CLUSTER`.

Избери една команда. Тя build-ва локален image, зарежда го в Kind, прилага patch
към Deployment-а, рестартира Pod-а и изчаква rollout:

```bash
make deploy-dev
```

Или за Delve debugging:

```bash
make deploy-debug
```

Dev използва `Dockerfile` и `imagescan-operator:dev`; debug използва `Dockerfile.debug`
и `imagescan-operator:debug`. `IMG` не променя тези фиксирани tags.
Настройките са в `config/manager/dev-mode-patch.yaml` и
`config/manager/debug-mode-patch.yaml`. Това са strategic merge patches за
`kubectl patch`, не самостоятелни ресурси за apply; не са включени в Kustomize.
Не са нужни нови зависимости. Не изпълнявай двата режима едновременно.

Стъпките са последователни и спират при грешка (не използвай `make -i`).
Вече изпълнените стъпки не се отменят автоматично. Patch при смяна на режим и
последващ restart могат да предизвикат последователни rollout промени.
Restart се изпълнява и при същия tag, за да се използва новозареденият image.

Targets не инсталират CRD и не създават test Pods. При промени по API полета
(например `unknown field "spec.severity"`) първо обнови локалните генерирани
файлове и инсталирай CRD в текущия клъстер:

```bash
make manifests generate
make install
```

След това изпълни избрания deploy target. След всеки rollout стартирай отново
port-forward; dev не използва порт 40000. Debug продължава със Step 6–8.
Step 3–5 остават ръчно обяснение и не се повтарят след `make deploy-debug`.
Новите targets не са изпълнявани срещу клъстера при редакцията на документа.

## Пълна dev тестова сесия с всички Pods

**1. Обнови оператора.** Командата build-ва, зарежда image и променя Deployment-а:

```bash
make deploy-dev
```

**2. Подготви workloads.** Командите създават/обновяват namespace и всички test Pods:

```bash
kubectl apply -f config/samples/imagescan-test-namespace.yaml
kubectl apply -f config/samples/imagescan-test-pod.yaml
kubectl apply -f config/samples/imagescan-test-deployment.yaml
kubectl apply -f config/samples/imagescan-test-extra-pods.yaml
```

Следващите команди само изчакват готовност и показват Pods. При грешка провери
проблемния Pod, преди да продължиш:

```bash
kubectl -n imagescan-test rollout status deployment/imagescan-test-nginx --timeout=120s
kubectl -n imagescan-test wait --for=condition=Ready pod -l app=web --timeout=120s
kubectl -n imagescan-test get pods -l app=web -o custom-columns='NAME:.metadata.name,IMAGES:.spec.containers[*].image'
```

Очакваме 6 Pods: `web-test`, 3 nginx реплики, BusyBox и Alpine — 3 уникални images,
ако няма други съвпадащи workloads (Step 15).

**3. Пусни наблюдение преди scan-а.** В отделен терминал тази команда само следи
ImageScan ресурсите; Ctrl+C спира наблюдението:

```bash
kubectl get imagescans -w
```

**4. Създай нов scan.** В основния терминал тези команди изтриват предишния тестов
ImageScan и го създават наново. Старите JSON отчети в PVC остават:

```bash
kubectl delete -f config/samples/imagescan-debug.yaml --ignore-not-found
kubectl apply -f config/samples/imagescan-debug.yaml
```

Името `imagescan-debug` не изисква debug build. Използвай един и същ namespace при
create/get/watch; manifest-ът няма зададен namespace. Следващата команда само
показва резултатите; изчакай всички да са `Completed`:

```bash
kubectl get imagescan imagescan-debug -o jsonpath='{.status.phase}{"\n"}{range .status.scans[*]}{.image}{"\t"}{.id}{"\t"}{.phase}{"\t"}{.message}{"\n"}{end}'
```

**5. Отвори HTTP тунела.** В отделен терминал командата отваря локален порт, без
промяна на ресурси. Остави я работеща; очаквай `Forwarding from 127.0.0.1:8080`:

```bash
kubectl -n imagescan-project-system port-forward service/imagescan-project-scan-results-service 8080:80 --address 127.0.0.1
```

**6. Провери HTTP.** В друг терминал тази GET заявка само чете отчети:

```bash
curl -i --max-time 15 'http://127.0.0.1:8080/scans?limit=2&offset=2'
```

Очаквай най-много 2 top-level отчета. Вложеният Trivy масив `Results` не е обект
на пагинацията. Step 17 съдържа останалите HTTP проверки и cleanup на test workloads.
При `Connection refused` към localhost първо провери port-forward терминала.

## Step 1: Разбери кои настройки запазихме

| Настройка       | Нормален dev режим       | Финален debug режим | Причина |
|-----------------|--------------------------|---------------------|---------|
| Image           | `imagescan-operator:dev` | `imagescan-operator:debug` | Debug образът съдържа Delve |
| Containercommand| `/manager`               | Премахната          | Позволява image ENTRYPOINT да стартира Delve |
| Memory limit    | `512Mi`                  | `512Mi`       | При стария лимит 128Mi наблюдавахме OOMKilled |
| Liveness probe  | `/healthz`               | Премахната | Паузата в debugger спира отговора; иначе kubelet рестартира контейнера |
| Readiness probe | `/readyz`                | Премахната | Иначе паузираният Pod изчезва от готовите Service endpoints |
| Leader election | `--leader-elect`         | Аргументът е премахнат | При пауза lease не се подновява и manager приключва |
| Webhook timeout | `10s`                    | `10s`      | Остава ограничение при breakpoint във webhook-а |

Според текущия `config/manager/manager.yaml`: CPU limit `500m`, memory limit
`512Mi`, requests CPU `100m`, memory `128Mi`. Patches по-долу използват тези стойности.
Security context не е променян за debugging: non-root, RuntimeDefault seccomp,
drop ALL capabilities, readOnlyRootFilesystem и без privilege escalation.
Не сме добавяли SYS_PTRACE или privileged режим.

Първо увеличихме probe failureThreshold на 100, но после премахнахме самите
probes. Не е нужно да повтаряш тези междинни настройки.

Debug настройките са в **живия Deployment в Kind**. Провереният
`config/manager/manager.yaml` все още съдържа нормалните probes, `/manager`,
`--leader-elect`, но вече има memory limit `512Mi`. Повторно deploy-ване може да промени debug настройките.

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

## Step 5: Приложи debug настройките

`make deploy-debug` от бързия старт изпълнява Step 3–6 автоматично.
Ако вече си го изпълнил, премини към логовете в Step 6.
Само при ръчно изпълнение на build/load тази команда променя Deployment-а,
използвайки YAML patch вместо дълъг inline JSON:

```bash
kubectl -n imagescan-project-system patch deployment imagescan-project-controller-manager --type=strategic --patch-file config/manager/debug-mode-patch.yaml
```

Patch-ът избира `:debug`, премахва command (за Delve ENTRYPOINT), probes и leader
election. При повторно зареждане на същия tag може да няма промяна в Pod template.
Тази команда подменя Pod-а, за да използва заредения image:

```bash
kubectl -n imagescan-project-system rollout restart deployment/imagescan-project-controller-manager
```

## Step 6: Провери стартирането

Тази команда само изчаква rollout-а:

```bash
kubectl rollout status deployment/imagescan-project-controller-manager \
  -n imagescan-project-system --timeout=120s
```

Без readiness probe успешният rollout **не доказва**, че manager и webhook
вече слушат. Прочети логовете:

```bash
kubectl logs \
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
kubectl port-forward \
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
(`scan := &scanv1.ImageScan{}`; номерата на редовете се променят).
Изключи webhook breakpoint-ите за този тест.

Следващите команди само проверяват тестовите ресурси и PVC:

```bash
kubectl get namespace imagescan-test --show-labels
kubectl get pods -n imagescan-test -l app=web
kubectl get pvc imagescan-results-pvc -n imagescan-project-system
```

Очакваме namespace `imagescan-test` с label `env=production`, Pod `web-test`
с label `app=web` и PVC `Bound`. Селекторите търсят **labels**, не имена.

Ако тестовият namespace или Pod липсва, тези команди ги създават/обновяват в Kind:

```bash
kubectl apply -f config/samples/imagescan-test-namespace.yaml
kubectl apply -f config/samples/imagescan-test-pod.yaml
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
  severity: MEDIUM,HIGH,CRITICAL
```

## Step 10: Създай или повтори scan-а

За тест с повече Pods първо изпълни подготовката в Step 15, после се върни тук.

За първото създаване командата записва ImageScan в Kind и задейства webhook-а:

```bash
kubectl apply -f config/samples/imagescan-debug.yaml
```

За ново сканиране със същия YAML първо изтрий стария тестов ImageScan, после го
създай отново. Това изтрива стария му status и може да премахне Jobs, които той
притежава. Запазените JSON файлове в PVC не се изтриват от тези команди.

```bash
kubectl delete -f config/samples/imagescan-debug.yaml --ignore-not-found
kubectl apply -f config/samples/imagescan-debug.yaml
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
kubectl logs \
  -n imagescan-project-system \
  deployment/imagescan-project-controller-manager -c manager -f
```

Тук се виждат `logf.FromContext(ctx).Info(...)` съобщенията. Ако breakpoint е
на самия logging ред, изпълни го с F10, преди да очакваш съобщението.
Върната от Reconcile грешка се логва от controller-runtime; самото
`return fmt.Errorf(...)` не е команда за отпечатване.

Тези команди само показват status-а и файловете в PVC:

```bash
kubectl get imagescan imagescan-debug -o yaml
kubectl exec -n imagescan-project-system \
  deployment/imagescan-project-controller-manager -c manager -- ls -lh /reports
```

За отваряне на JSON във VS Code замени `REPORT_ID.json` с реалното име.
Следващата команда копира съдържанието в WSL `/tmp`; презаписва локалното копие,
но не променя файла в PVC:

```bash
kubectl exec -n imagescan-project-system \
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
kubectl rollout restart deployment/imagescan-project-controller-manager \
  -n imagescan-project-system
```

Повтори Step 6 (логове), Step 7 (нов port-forward) и Step 8 (нов attach).
При напускане на сесията предпочитай **Disconnect** и остави процеса да работи.
**Stop** може да приключи manager-а. Не оставяй процеса паузиран при disconnect.

За диагностика следващите команди само четат Pod status и Service endpoints:

```bash
kubectl get pods -n imagescan-project-system -l control-plane=controller-manager
kubectl get endpointslices -n imagescan-project-system \
  -l kubernetes.io/service-name=imagescan-project-webhook-service
```

## Step 14: Върни нормалния dev режим

Приключи debug сесията и спри старите port-forward процеси с Ctrl+C.
Следващата команда build-ва dev образа, зарежда го в Kind, прилага
`config/manager/dev-mode-patch.yaml`, рестартира Deployment-а и изчаква rollout:

```bash
make deploy-dev
```

Тя променя клъстера: избира `:dev`, задава `/manager`, възстановява leader election
и двете probes. Използва memory limit `512Mi` и requests `100m`/`128Mi`.
Dev Dockerfile също има ENTRYPOINT `/manager`; patch-ът възстановява целия dev режим.
Прочети логовете от Step 6 и стартирай HTTP port-forward от Step 16 отново.
За dev режим не се използват Delve attach и порт 40000.

## Step 15: Подготви повече Pods за тест на уникалните images

Изпълни тази подготовка **преди** създаване на нов ImageScan в Step 10.
Новите файлове са отделни samples и се прилагат изрично, а не чрез
`config/samples/kustomization.yaml`:

| Файл | Ресурси | Image |
|---|---|---|
| `imagescan-test-pod.yaml` (съществуващ) | 1 Pod `web-test` | `nginx:1.27` |
| `imagescan-test-deployment.yaml` | Deployment с 3 реплики | `nginx:1.27` |
| `imagescan-test-extra-pods.yaml` | 2 отделни Pods | `busybox:1.37`, `alpine:3.21` |

Всички Pods имат label `app=web` и namespace `imagescan-test` с `env=production`.
Допълнителният label `imagescan-test-workload=nginx-replicas` ограничава Deployment
селектора до неговите Pods. BusyBox и Alpine изпълняват цикъл със `sleep`, за да
останат работещи. Не са нужни нови Go зависимости или Service за тези Pods:
операторът чете image имената от Pod спецификациите, а не изпраща HTTP към тях.
Тези image tags служат за тестови входове, не за препоръка за production версии.

Следващите команди създават/обновяват namespace и тестовите workloads в Kind:

```bash
kubectl apply -f config/samples/imagescan-test-namespace.yaml
kubectl apply -f config/samples/imagescan-test-pod.yaml
kubectl apply -f config/samples/imagescan-test-deployment.yaml
kubectl apply -f config/samples/imagescan-test-extra-pods.yaml
```

Тези команди само изчакват създаването/готовността и показват image имената.
Изчакай Deployment-а, за да съществуват и трите негови Pods преди scan-а:

```bash
kubectl -n imagescan-test rollout status deployment/imagescan-test-nginx --timeout=120s
kubectl -n imagescan-test wait --for=condition=Ready pod -l app=web --timeout=120s
kubectl -n imagescan-test get pods -l app=web -o custom-columns='NAME:.metadata.name,IMAGES:.spec.containers[*].image'
```

При timeout прегледай състоянието и events на проблемния Pod, преди да продължиш.
При липса на други съвпадащи Pods очакваме **6 Pods и 3 уникални image имена**.
Четирите nginx Pods трябва да дадат един запис за `nginx:1.27` в `status.scans`.
Контролерът сравнява точния string: `nginx:1.27` и `docker.io/library/nginx:1.27`
биха били различни ключове. Той включва и init/ephemeral containers, ако има такива.
Namespace селекторът обхваща всички namespaces с `env=production`, затова други
съвпадащи workloads могат да увеличат броя резултати.

Сега изпълни Step 10 за нов scan. Откриването на images става само когато
`status.phase` е празен; добавяне на Pods след това или обикновен повторен `apply`
не обновява списъка. За HTTP теста остави manager да работи с Continue/F5.

Следващите команди само показват scan status и текущите Jobs. Използвай същия
namespace за ImageScan като при Step 10 (sample файлът няма зададен namespace):

```bash
kubectl get imagescan imagescan-debug -o jsonpath='{.status.phase}{"\n"}{range .status.scans[*]}{.image}{"\t"}{.id}{"\t"}{.phase}{"\n"}{end}'
kubectl -n imagescan-project-system get jobs
```

Изчакай всички три записа да са `Completed`. При `Failed` прегледай `status.scans[].message`
и manager логовете. Завършените/неуспешните scan Jobs се изтриват от контролера,
така че празен списък с Jobs сам по себе си не доказва успех.

## Step 16: Обнови избрания режим и отвори HTTP достъпа

Ако вече си изпълнил `make deploy-dev` или `make deploy-debug` за текущия код,
премини направо към port-forward. След промяна по кода избери една команда:

- `make deploy-dev` за нормален HTTP тест;
- `make deploy-debug` за Delve/VS Code debugging.

Всяка build-ва, зарежда image в Kind, прилага режима, рестартира manager и изчаква
rollout. Само build/load/restart не сменя режима на Deployment-а.
След подмяна на Pod-а възстанови тунелите; в debug режим провери логовете от Step 6.
Без readiness probe успешният rollout не доказва готов HTTP server.

В **терминал 1**, само ако ще използваш debugger, тази команда отваря локален
тунел към Delve, без да променя Kubernetes ресурси:

```bash
kubectl -n imagescan-project-system port-forward deployment/imagescan-project-controller-manager 40000:40000 --address 127.0.0.1
```

Не я стартирай втори път, ако тунелът от Step 7 още работи.
В **терминал 2** следващата команда отваря HTTP тунела през Service:

```bash
kubectl -n imagescan-project-system port-forward service/imagescan-project-scan-results-service 8080:80 --address 127.0.0.1
```

Пътят е `127.0.0.1:8080 → Service:80 → manager:8082`.
`40000` е Delve, `8081` е health, а `8443` е metrics; те не са HTTP API за reports.
Остави терминалите отворени. При подмяна на manager Pod-а рестартирай двата тунела.
Ако локалният 8080 е зает, използвай `18080:80` и порт 18080 в curl заявките.

Ако Service липсва или няма подходящ Pod, тези команди само четат конфигурацията
и endpoints за диагностика:

```bash
kubectl -n imagescan-project-system get service imagescan-project-scan-results-service -o yaml
kubectl -n imagescan-project-system get endpointslices -l kubernetes.io/service-name=imagescan-project-scan-results-service
```

Service е включен в `config/default/kustomization.yaml`; името получава prefix
`imagescan-project-`. Прилагане само на суровия `scan_results_service.yaml` няма
да добави този prefix или namespace. Повторен пълен deploy може да върне нормалната
manager конфигурация — след него провери Step 5 отново.

## Step 17: HTTP проверки в трети WSL терминал

Всички curl команди по-долу изпращат само GET заявки и не променят ресурси или
отчети. `-i` показва HTTP статуса; `--max-time 15` ограничава чакането.
Паузиран в debugger manager не може да обслужва HTTP — натисни Continue/F5.

Първо провери списъка с reports; очакваният успешен отговор е `200` и JSON масив:

```bash
curl -i --max-time 15 'http://127.0.0.1:8080/scans'
```

Всеки елемент има `scanID` и `report` (оригиналния Trivy JSON).
По подразбиране `limit=20`, `offset=0`. Празен масив `[]` означава, че няма
прочетени валидни JSON отчети; не доказва приключил scan. API чете всички
`/reports/*.json`, включително стари сканирания от PVC, а не само текущия ImageScan.
Невалиден/все още недописан JSON се пропуска от списъка и се логва.

За конкретен отчет първо прочети реалните IDs от status с командата в Step 15.
Замени `REAL_SCAN_ID` с ID на `Completed` запис; заявката очаква `200` и директния
Trivy JSON, без обвивката `scanID`/`report` от списъка:

```bash
curl -i --max-time 15 'http://127.0.0.1:8080/scans/REAL_SCAN_ID'
```

Следващите заявки проверяват филтриране и две страници, без промяна на данните:

```bash
curl -i --max-time 15 'http://127.0.0.1:8080/scans?imagePrefix=nginx&limit=20&offset=0'
curl -i --max-time 15 'http://127.0.0.1:8080/scans?limit=2&offset=0'
curl -i --max-time 15 'http://127.0.0.1:8080/scans?limit=2&offset=2'
```

`imagePrefix` е чувствителен към регистъра prefix на `report.ArtifactName`.
Ако Trivy е записал пълно registry име, използвай неговия prefix.
Филтрирането е преди `offset`/`limit`; редът е по имената на JSON файловете,
не по дата. При точно 3 налични валидни отчета страниците съдържат 2 и 1 елемент.
При стари отчети в PVC броят може да е по-голям; за текущия тест сравнявай IDs
от ImageScan status. Не изтривай PVC или стари reports за тази проверка.

Следващите GET заявки проверяват грешки и празен резултат:

```bash
curl -i --max-time 15 'http://127.0.0.1:8080/scans?limit=0'
curl -i --max-time 15 'http://127.0.0.1:8080/scans?limit=abc'
curl -i --max-time 15 'http://127.0.0.1:8080/scans?offset=-1'
curl -i --max-time 15 'http://127.0.0.1:8080/scans?imagePrefix=imagescan-no-such-image-'
curl -i --max-time 15 'http://127.0.0.1:8080/scans/nonexistent-test-id'
```

Очаквания според текущия код: първите три връщат `400`; несъвпадащият prefix
връща `200` и `[]`; несъществуващият ID връща `404`. `500` от `/scans` означава,
че директорията `/reports` не може да се прочете. Timeout или connection refused
изисква проверка на manager, debugger и port-forward, а не на scan филтрите.

### Премахване само на новите тестови workloads

След приключване тази команда изтрива новия Deployment (и неговите Pods) и двата
допълнителни Pods. Запазва `web-test`, namespace, ImageScan и отчетите в PVC:

```bash
kubectl delete -f config/samples/imagescan-test-deployment.yaml -f config/samples/imagescan-test-extra-pods.yaml --ignore-not-found
```
