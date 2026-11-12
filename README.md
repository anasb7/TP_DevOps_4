# GuestBook Observabilité

Votre objectif est de rendre le guestbook du Jour 3 observable: métriques, dashboards, SLO, alertes, logs et traces, puis de provoquer des pannes pour vérifier que vous les voyez. Et d'apprendre un maximum pendant cette session.

Ce dépôt contient tout ce qu'il faut pour déployer le guestbook, pour vous concentrer sur l'observabilité :

```text
.
├── cours/                        les slides du Jour 4
├── guestbook/                    le code du guestbook v1.0.0
│   ├── main.go                   routes et handlers
│   ├── metrics.go                métriques Prometheus et logs JSON         (ex. 0, 3)
│   ├── tracing.go                traces OpenTelemetry, désactivées          (ex. 9)
│   └── Dockerfile
├── deploy/
│   ├── namespaces.yaml           namespaces guestbook et data               (ex. 0)
│   ├── redis/                    Redis et son volume                        (ex. 0)
│   ├── guestbook/                chart Helm du guestbook                    (ex. 0, 1, 3, 9, 10)
│   └── k6/                       test de charge, Job k6 de 17 minutes       (ex. 4, 5, 6)
├── exercise-5/
│   └── prometheusrule.yaml       point de départ de l'exercice 5
└── README.md
```

Pour chaque exercice, créez un dossier `exercise-N/` avec un `README.md` (votre doc et votre fiche d'apprentissage) et les fichiers que vous écrivez (valeurs de charts, ressources de monitoring, expériences de chaos...). Le guestbook, son chart et le test de charge restent dans `guestbook/` et `deploy/`: quand vous les modifiez, notez dans le `README.md` de l'exercice ce que vous avez changé et pourquoi (ou le `git diff`).

## Fiche d'apprentissages

Pour l'évaluation, merci de remplir une fiche d'apprentissage pour chaque exercice réalisé, dans le `README.md` de son dossier `exercise-N/`.
Cette fiche aura le template suivant :

```markdown
# Apprentissages

## Titre d'exo

Court résumé de l’objectif visé

### Problème rencontré et pourquoi il est survenu

### Solution appliquée et pourquoi cette solution fonctionne

### Ce que j'ai appris

```

## N'oubliez pas !

Tenez votre doc dans le `README.md` de chaque `exercise-N/`, pour garder une trace de ce que vous faites, des commandes que vous utilisez, des erreurs que vous rencontrez et comment vous les avez résolues.

---

## 0. Préparer votre environnement

### Pourquoi

Prometheus, Grafana, Loki et Chaos Mesh consomment des ressources, et la résilience ne se teste pas sur un seul nœud. Il vous faut un cluster plus costaud qu'au Jour 3, et une nouvelle version du guestbook qui expose ses métriques.

### Comment

**Fichiers**: `guestbook/`, `deploy/namespaces.yaml`, `deploy/redis/`, `deploy/guestbook/`

1. Installez les outils suivants, en plus de ceux du Jour 3 (minikube, kubectl, Docker, Helm, k9s, stern, kubens) :

  - [Go](https://go.dev/doc/install): pour modifier le guestbook
  - [k6](https://grafana.com/docs/k6/latest/set-up/install-k6/) (optionnel): pour lancer les tests de charge depuis votre poste

2. Créez un cluster minikube de 2 nœuds :

  ```bash
  minikube start -p observability --nodes 2 --cpus 3 --memory 4096
  minikube -p observability addons enable metrics-server
  minikube -p observability addons enable ingress
  ```

3. Construisez le guestbook `v1.0.0` et chargez-le dans minikube :

  ```bash
  docker build -t guestbook:v1.0.0 guestbook/
  minikube -p observability image load guestbook:v1.0.0
  ```

4. Déployez Redis et le guestbook :

  ```bash
  kubectl apply -f deploy/namespaces.yaml
  kubectl apply -n data -f deploy/redis/
  helm install guestbook deploy/guestbook -n guestbook
  ```

### Indices

- `minikube profile list` affiche vos clusters, `minikube profile observability` change le profil par défaut
- Arrêtez le cluster du Jour 3 pour libérer de la mémoire: `minikube stop`

### Vérifications

- [ ] `kubectl get nodes` affiche 2 nœuds `Ready`
- [ ] Le guestbook `v1.0.0` fonctionne et est connecté à Redis
- [ ] `curl` sur `/metrics` (via port-forward) retourne des métriques au format Prometheus
- [ ] Je sais lire `guestbook/metrics.go`: quelles métriques et quels champs de logs sont fournis ?

## 1. Répartir les pods (Topology Spread Constraints)

### Pourquoi

Le scheduler place les pods là où il y a de la place, pas forcément de manière répartie: si les 3 replicas sont sur le même nœud et que ce nœud tombe, le guestbook tombe avec lui. Les [Topology Spread Constraints](https://kubernetes.io/docs/concepts/scheduling-eviction/topology-spread-constraints/) contrôlent cette répartition.

### Comment

**Fichiers**: `deploy/guestbook/templates/deployment.yaml`, `deploy/guestbook/values.yaml`

1. **Observez la répartition actuelle** des pods sur les nœuds
2. **Ajoutez des Topology Spread Constraints** au Deployment du chart `deploy/guestbook`
3. **Déployez** avec `helm upgrade` et observez la nouvelle répartition
4. **Simulez une panne de nœud**: `kubectl drain` sur le nœud qui porte le plus de pods, et vérifiez que le guestbook reste accessible
5. **Remettez le nœud en service** et observez: les pods se rééquilibrent-ils tout seuls ?

### Indices

- `kubectl get pods -o wide` affiche le nœud de chaque pod
- La clé de topologie d'un nœud est le label `kubernetes.io/hostname`
- `maxSkew: 1`: au plus 1 pod de différence entre deux nœuds
- `whenUnsatisfiable: DoNotSchedule` laisse le pod en `Pending`, `ScheduleAnyway` est une contrainte souple
- Redis n'a qu'un replica: que se passe-t-il pour lui pendant le drain ?

### Vérifications

- [ ] Mes pods sont répartis sur les 2 nœuds
- [ ] Le guestbook reste accessible pendant le drain d'un nœud
- [ ] Je sais expliquer la différence entre `DoNotSchedule` et `ScheduleAnyway`

## 2. Déployer la stack Prometheus

### Pourquoi

[kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack) installe en un chart Prometheus, Alertmanager, Grafana, l'opérateur Prometheus et des dashboards Kubernetes prêts à l'emploi.

### Comment

**Fichiers**: `exercise-2/monitoring.yaml` (à créer)

1. **Ajoutez le repository Helm** `prometheus-community`
2. **Installez `kube-prometheus-stack`** dans un namespace `monitoring`, avec vos valeurs dans `exercise-2/monitoring.yaml`
3. **Accédez aux interfaces** de Prometheus, Alertmanager et Grafana (port-forward)
4. **Explorez les dashboards Kubernetes par défaut**: consommation des nœuds, des namespaces, des pods du guestbook
5. **Explorez Prometheus**: la page `Status > Targets`, et quelques requêtes PromQL (`up`, `node_memory_MemAvailable_bytes`)

### Indices

- Le mot de passe admin de Grafana est dans un Secret du namespace `monitoring`
- `kubectl get crd | grep monitoring.coreos.com` liste les CRDs installées par l'opérateur
- Regardez les valeurs `serviceMonitorSelectorNilUsesHelmValues` et `ruleSelectorNilUsesHelmValues` du chart: elles serviront aux exercices 3 et 5
- Sur minikube, certaines targets du control plane restent `DOWN`: cherchez pourquoi, et désactivez-les dans vos valeurs

### Vérifications

- [ ] Les targets Prometheus sont `UP`, et je sais expliquer celles que j'ai désactivées
- [ ] Grafana affiche les dashboards Kubernetes
- [ ] Je sais retrouver la consommation CPU et mémoire des pods du guestbook

## 3. Instrumenter le guestbook

### Pourquoi

Les dashboards par défaut montrent les ressources (méthode USE), pas ce que vivent les utilisateurs. Pour cela, l'application doit exposer ses propres métriques. La version `v1.0.0` expose déjà `http_requests_total` et `guestbook_redis_up`, mais il manque la latence: c'est la seule modification du code de ce TP.

### Comment

**Fichiers**: `guestbook/metrics.go`, `exercise-3/servicemonitor.yaml` (à créer)

1. **Lisez le code** de `guestbook/metrics.go`: comment `http_requests_total` est-elle déclarée et incrémentée ? Pourquoi le middleware utilise-t-il `recover()` ?
2. **Ajoutez un histogramme `http_request_duration_seconds`** avec les labels `handler` et `method`
   - Choisissez des buckets adaptés au SLO de latence de l'exercice 5: 95% des lectures et écritures de messages (`/lrange`, `/rpush`) en moins de 400 ms
3. **Construisez l'image `guestbook:v1.1.0`**, chargez-la dans minikube et déployez-la (`helm upgrade ... --set image.tag=v1.1.0`)
4. **Faites scraper le guestbook par Prometheus**: écrivez un ServiceMonitor (`exercise-3/servicemonitor.yaml`) et appliquez-le dans le namespace `guestbook`
5. **Vérifiez** dans Prometheus que la target du guestbook est `UP` et que vos métriques remontent

### Indices

- La documentation de [client_golang](https://pkg.go.dev/github.com/prometheus/client_golang/prometheus) décrit `NewHistogramVec` et `NewTimer`
- Un ServiceMonitor sélectionne des Services par leurs labels, et un port par son **nom**
- Le ServiceMonitor doit aussi être sélectionné par Prometheus: revoyez les valeurs de l'exercice 2
- Attention à la cardinalité: utilisez le nom de la route (`/lrange/{key}`), pas l'URL réelle

### Vérifications

- [ ] `/metrics` expose `http_request_duration_seconds_bucket`
- [ ] La target du guestbook est `UP` dans Prometheus, avec une entrée par pod
- [ ] Je sais expliquer la différence entre un counter, une gauge et un histogramme

## 4. Construire un dashboard RED

### Pourquoi

La méthode RED (Rate, Errors, Duration) résume la santé d'un service vue par ses utilisateurs. Un dashboard RED est le premier écran qu'on ouvre pendant un incident.

### Comment

**Fichiers**: `deploy/k6/`, `exercise-4/` (captures et analyse)

1. **Créez un dashboard** `Guestbook` dans Grafana avec 3 panels :
   - **Rate**: requêtes par seconde, par handler
   - **Errors**: pourcentage de réponses 5xx
   - **Duration**: p50, p95 et p99 de la latence
2. **Lancez le test de charge** fourni (17 minutes) et observez le dashboard, ainsi que le HPA

   ```bash
   kubectl create configmap k6-script -n guestbook --from-file=deploy/k6/load-test.js
   kubectl apply -f deploy/k6/k6-job.yaml
   ```
3. **Corrélez** avec les dashboards Kubernetes: quelle ressource sature en premier ?
4. **Rendez dans votre doc** :
   - Une capture du dashboard **pendant** le test de charge, période visible (montée du rate, effet du HPA)
   - La requête PromQL de chaque panel
   - 2 ou 3 phrases d'analyse: ce qui a saturé en premier, et à quel moment

### Indices

- `rate()` s'applique à un counter, sur une fenêtre au moins 4 fois plus longue que l'intervalle de scrape
- `histogram_quantile()` s'applique à `sum by (le) (rate(..._bucket[5m]))`
- Pourquoi un p99 plutôt qu'une moyenne ? Comparez les deux pendant la charge
- Les probes du kubelet (`/healthz`) et les scrapes de Prometheus (`/metrics`) ne sont pas des utilisateurs: excluez-les de vos panels (`handler!~"..."`)
- Sans aucune erreur, le panel Errors affiche "No data" plutôt que 0: pourquoi ? Cherchez `or vector(0)`

### Vérifications

- [ ] Mon dashboard affiche le rate, le taux d'erreur et les percentiles de latence
- [ ] Je vois l'effet du test de charge et du HPA sur le dashboard
- [ ] Ma doc contient la capture, les requêtes PromQL et l'analyse

## 5. Définir un SLO et alerter dessus

### Pourquoi

Alerter sur chaque pic d'erreurs épuise l'équipe, alerter quand le budget du mois est épuisé arrive trop tard. On alerte sur la vitesse de consommation de l'error budget (burn rate), et chaque alerte pointe vers un runbook.

### Comment

**Fichiers**: `exercise-5/prometheusrule.yaml`, `exercise-5/alertmanager.yaml` (à créer)

1. **Définissez les SLO** du guestbook dans votre doc :
   - Disponibilité: 99% des requêtes sans erreur 5xx sur 30 jours
   - Latence: 95% des lectures et écritures de messages (`/lrange`, `/rpush`) en moins de 400 ms
2. **Lisez la PrometheusRule fournie** (`exercise-5/prometheusrule.yaml`): les recording rules du taux d'erreur sur 5m, 30m, 1h et 6h, et l'alerte burn rate rapide (1h et 5m). Que représente le `14.4` ?
3. **Complétez-la** :
   - l'alerte burn rate lente (6h et 30m), avec son annotation `runbook_url`
   - une alerte `GuestbookDown`: toutes les instances du guestbook sont down, ou il n'y en a plus aucune
   - une alerte sur la cause, avec la gauge `guestbook_redis_up` fournie par le guestbook (mise à jour par `/healthz`)
   - remplacez `<your-account>` dans les `runbook_url`: elles doivent pointer vers le runbook de votre dépôt (`exercise-7/`)
4. **Configurez Alertmanager** (`exercise-5/alertmanager.yaml`, à passer en plus de `exercise-2/monitoring.yaml` au `helm upgrade` de la stack): routez les alertes du namespace `guestbook` vers un receiver de votre choix (webhook, Slack, Discord...)
5. **Provoquez l'alerte burn rate**: relancez le test k6, attendez la fin de la montée en charge (1 minute), puis remplacez la liste des messages par une chaîne (`redis-cli set guestbook oops`). Suivez l'alerte de `inactive` à `pending` puis `firing`, puis sa résolution après `redis-cli del guestbook`
6. **Coupez Redis** (0 replica): quelles alertes se déclenchent ? Que voient les utilisateurs, et que voient les métriques du guestbook ? Pourquoi ?

### Indices

- Taux d'erreur observé = burn rate × (100% - SLO): pour un SLO de 99% et un burn rate de 14,4, l'alerte rapide se déclenche à 14,4% d'erreurs
- Les PrometheusRules doivent être sélectionnées par Prometheus: revoyez les valeurs de l'exercice 2
- Le champ `for:` évite d'alerter sur un pic isolé
- Une alerte ne sert que si elle arrive à Alertmanager avec le label sur lequel vous routez: vérifiez les labels de vos alertes dans Prometheus (`Alerts`), en particulier après un `sum`, un `max` ou un `absent()`
- L'alerte rapide met quelques minutes à se déclencher, et autant à se résoudre: le test k6 doit tourner jusqu'à la résolution. Sans trafic, le taux d'erreur n'a plus de valeur et l'alerte se résout pour une mauvaise raison
- Pour relancer le test k6: `kubectl delete job -n guestbook k6-load-test`, puis `kubectl apply -f deploy/k6/k6-job.yaml`
- Pourquoi les recording rules fournies excluent-elles `/healthz` et `/metrics` ?
- Sans receiver externe, [webhook.site](https://webhook.site) ou un simple pod `echo-server` font l'affaire
- `amtool` (dans le pod Alertmanager) permet de tester le routage et de poser des silences

### Vérifications

- [ ] Mes alertes apparaissent dans Prometheus (`Status > Rules`, `Alerts`), avec le label `namespace`
- [ ] L'alerte burn rate passe en `firing` quand le guestbook renvoie des 500, et se résout ensuite
- [ ] Je sais expliquer pourquoi l'alerte burn rate ne se déclenche pas quand Redis est coupé
- [ ] L'alerte arrive dans mon receiver, avec son `runbook_url`
- [ ] Je sais expliquer pourquoi on alerte sur le symptôme plutôt que sur la cause

## 6. Provoquer des pannes avec Chaos Mesh

### Pourquoi

Nos alertes se déclenchent-elles vraiment ? Le guestbook survit-il vraiment à la perte d'un pod ? On ne le saura qu'en provoquant la panne, de façon contrôlée. [Chaos Mesh](https://chaos-mesh.org/) décrit les pannes en CRDs.

### Comment

**Fichiers**: `exercise-6/chaos-mesh.yaml`, et vos expériences dans `exercise-6/` (à créer)

1. **Installez Chaos Mesh** avec son chart Helm dans un namespace `chaos-mesh` (valeurs dans `exercise-6/chaos-mesh.yaml`)
2. **Écrivez votre hypothèse** dans votre doc: *"si un pod du guestbook est tué, le taux d'erreur reste sous 1% et aucune alerte ne se déclenche"*
3. **Créez une expérience `PodChaos`** (`pod-kill`, dans `exercise-6/`) qui cible un pod du guestbook, puis un `Schedule` qui la rejoue toutes les 2 minutes
4. **Relancez le test de charge k6** et observez le dashboard RED et les alertes pendant les expériences
5. **Concluez**: l'hypothèse est-elle vérifiée ? Sinon, corrigez (probes, replicas, Topology Spread Constraints...) et recommencez
6. **Allez plus loin**: une expérience `NetworkChaos` qui ajoute 500 ms de latence aux réponses de Redis. Observez le panel Duration du dashboard: la latence affichée correspond-elle aux 500 ms injectés ? Pourquoi ?

### Indices

- Le chart suppose que les conteneurs tournent avec Docker. Vérifiez le runtime de vos nœuds (`kubectl get nodes -o wide`, colonne `CONTAINER-RUNTIME`) et adaptez les valeurs `chaosDaemon.runtime` et `chaosDaemon.socketPath`
- Le guestbook joint Redis par l'adresse de son Service, pas par l'IP du pod: une `NetworkChaos` qui filtre sur l'IP du pod Redis côté guestbook n'aura aucun effet
- Si une expérience reste bloquée à la suppression, regardez ses `finalizers`
- Ciblez les pods avec `selector.labelSelectors` et `mode: one`
- Limitez le *blast radius*: un seul namespace, une durée courte
- Supprimez le `Schedule` à la fin de l'exercice

### Vérifications

- [ ] Chaos Mesh tue un pod du guestbook selon le planning
- [ ] L'impact (ou l'absence d'impact) est visible sur le dashboard RED
- [ ] J'ai validé ou invalidé mon hypothèse, et je sais pourquoi

## 7. Écrire un runbook et un post-mortem

### Pourquoi

Une alerte sans runbook réveille quelqu'un qui ne sait pas quoi faire. Un incident sans post-mortem se reproduit. Les deux s'écrivent en markdown, à côté du code.

### Comment

**Fichiers**: `exercise-7/guestbook-error-budget-burn.md`, `exercise-7/postmortem.md` (à créer)

1. **Écrivez un runbook** `exercise-7/guestbook-error-budget-burn.md`, vers lequel pointe le `runbook_url` de l'exercice 5 :
   - Ce que signifie l'alerte et son impact utilisateur
   - Les premiers diagnostics (dashboards, requêtes PromQL, commandes `kubectl`)
   - Les actions de mitigation
2. **Écrivez le post-mortem blameless** (`exercise-7/postmortem.md`) de l'incident Redis de l'exercice 5 ou de l'expérience `NetworkChaos` de l'exercice 6 :
   - Résumé, impact (durée, % d'erreurs, budget consommé), timeline, cause racine, ce qui a bien marché, actions correctives avec un responsable

### Indices

- Le post-mortem s'appuie sur vos captures du dashboard RED et l'historique des alertes
- Blameless: on décrit ce que le système a permis, pas qui s'est trompé

### Vérifications

- [ ] Un collègue peut suivre mon runbook sans m'appeler
- [ ] Mon post-mortem contient une timeline et des actions correctives concrètes

## 8. (Optionnel) Centraliser les logs avec Loki

### Pourquoi

Les métriques disent qu'il y a un problème, les logs disent pourquoi. Mais `kubectl logs` ne montre que les pods vivants, un par un. Loki centralise les logs de tout le cluster et les rend interrogeables depuis Grafana.

### Comment

**Fichiers**: `exercise-8/loki.yaml`, `exercise-8/alloy.yaml` (à créer)

1. **Installez Loki** (mode `SingleBinary`) et **Grafana Alloy** pour collecter les logs des pods (Promtail, son prédécesseur, est déprécié)
2. **Ajoutez Loki** comme datasource dans Grafana
3. **Explorez les logs JSON du guestbook** (champs `level`, `handler`, `code`, `duration_ms`, `version`, `error`, `trace_id`) dans Grafana
4. **Requêtez** en LogQL: les erreurs du guestbook pendant une expérience de chaos, le nombre de requêtes lentes par minute
5. **Ajoutez un panel de logs** au dashboard RED

### Indices

- Le collecteur tourne en DaemonSet (cf. Jour 3): il lit les logs des pods sur chaque nœud
- `| json` extrait les champs d'un log structuré
- N'ajoutez pas de label Loki à forte cardinalité (identifiant de requête, utilisateur...)

### Vérifications

- [ ] Je retrouve les logs d'un pod tué par Chaos Mesh
- [ ] Je filtre les logs du guestbook par niveau et par code HTTP

## 9. (Optionnel) Tracer les requêtes avec OpenTelemetry

### Pourquoi

Une trace suit une requête à travers tous les composants qu'elle traverse et montre où le temps est passé. OpenTelemetry est le standard pour produire traces, métriques et logs.

### Comment

**Fichiers**: `guestbook/tracing.go` (lecture), `deploy/guestbook/templates/configmap.yaml`

1. **Lisez `guestbook/tracing.go`**: le guestbook est déjà instrumenté (un span par requête HTTP, un span par commande Redis), mais le tracing est désactivé
2. **Installez Grafana Tempo** et ajoutez-le comme datasource dans Grafana
3. **Activez le tracing** du guestbook en lui donnant l'adresse OTLP de Tempo
4. **Relancez l'expérience `NetworkChaos`** et retrouvez dans une trace le span Redis qui prend 500 ms

### Indices

- L'endpoint OTLP se configure avec les variables standard `OTEL_EXPORTER_OTLP_ENDPOINT` et `OTEL_SERVICE_NAME`: mettez-les dans la ConfigMap du chart
- Le guestbook exporte en OTLP/HTTP (port `4318`), pas en gRPC (`4317`)
- Pourquoi les handlers passent-ils `req.Context()` à Redis, et pas un contexte global ?
- Les logs JSON contiennent un champ `trace_id`: configurez le lien logs → traces dans la datasource Loki (*derived fields*)

### Vérifications

- [ ] Je vois les traces du guestbook dans Grafana
- [ ] Je passe d'une ligne de log à la trace correspondante

## 10. (Optionnel) Surveiller de l'extérieur (Blackbox exporter)

### Pourquoi

Les métriques internes (white-box) ne voient pas une panne de l'Ingress ou du DNS. Une sonde externe (black-box) teste l'application comme un utilisateur.

### Comment

**Fichiers**: `exercise-10/blackbox.yaml`, `exercise-10/probe.yaml`, `exercise-10/prometheusrule-blackbox.yaml` (à créer)

1. **Installez `prometheus-blackbox-exporter`** avec son chart Helm
2. **Créez une `Probe`** (CRD de l'opérateur Prometheus, `exercise-10/probe.yaml`) qui teste `http://guestbook.local/lrange/guestbook` via l'Ingress toutes les 30 s, comme le fait l'UI
3. **Ajoutez une alerte** sur `probe_success == 0` (une PrometheusRule `exercise-10/prometheusrule-blackbox.yaml`)
4. **Cassez l'Ingress** (désactivez-le dans `values.yaml`): quelle alerte se déclenche, la vôtre ou celles de l'exercice 5 ?

### Indices

- Le module `http_2xx` du blackbox exporter vérifie un code HTTP 2xx
- Pourquoi ne pas tester `/healthz` ? Essayez avec l'Ingress désactivé
- Depuis le cluster, `guestbook.local` doit résoudre vers l'Ingress controller: utilisez son Service comme cible et un header `Host`, ou ajoutez une entrée à CoreDNS

### Vérifications

- [ ] `probe_success` vaut 1 quand tout va bien
- [ ] L'alerte blackbox se déclenche quand l'Ingress est cassé, alors que les métriques internes restent normales

## Objectifs pédagogiques

### Savoir

- [ ] Je sais expliquer la différence entre monitoring et observabilité
- [ ] Je connais les 3 signaux (métriques, logs, traces) et à quoi sert chacun
- [ ] Je connais les méthodes USE, RED et les Golden Signals
- [ ] Je sais pourquoi on utilise des percentiles plutôt que des moyennes
- [ ] Je sais expliquer SLI, SLO, error budget et burn rate
- [ ] Je sais pourquoi on alerte sur les symptômes plutôt que sur les causes
- [ ] Je connais l'architecture de Prometheus (pull, TSDB, Alertmanager, opérateur)
- [ ] Je sais à quoi servent un runbook et un post-mortem blameless
- [ ] Je connais les principes du chaos engineering (steady state, hypothèse, blast radius)

### Savoir-faire

- [ ] Je sais répartir des pods sur plusieurs nœuds avec des Topology Spread Constraints
- [ ] Je sais déployer kube-prometheus-stack et explorer ses dashboards
- [ ] Je sais instrumenter une application et la faire scraper avec un ServiceMonitor
- [ ] Je sais écrire des requêtes PromQL (`rate`, `sum by`, `histogram_quantile`)
- [ ] Je sais construire un dashboard Grafana et l'analyser
- [ ] Je sais écrire des alertes burn rate et les router avec Alertmanager
- [ ] Je sais lancer une expérience de chaos et en analyser l'impact
- [ ] Je sais écrire un runbook et un post-mortem

## Conseils pour aller plus loin

### Ressources d'apprentissage
- **Google SRE Book et Workbook**: <https://sre.google/books/>
- **Documentation Prometheus**: <https://prometheus.io/docs/>
- **OpenTelemetry**: <https://opentelemetry.io/docs/>
- **Principles of Chaos Engineering**: <https://principlesofchaos.org/>

### Prochaines étapes
1. **Générez vos alertes SLO** avec [Sloth](https://sloth.dev/) ou [Pyrra](https://github.com/pyrra-dev/pyrra)
2. **Collectez métriques, logs et traces avec un seul agent** (Grafana Alloy ou OpenTelemetry Collector)
3. **Rejouez vos expériences de chaos** régulièrement, en dehors des heures de pointe

### Bonnes pratiques à retenir
- Alertez sur les symptômes vus par l'utilisateur, avec un runbook pour chaque alerte
- Surveillez la cardinalité de vos métriques
- Versionnez dashboards et alertes avec l'application
- Préférez les percentiles aux moyennes
- Limitez le blast radius de vos expériences de chaos
