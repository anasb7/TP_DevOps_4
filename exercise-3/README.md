# Exercice 3 : instrumenter le guestbook

## Doc

### Objectif

Ajouter un histogramme de latence au code du guestbook, construire la `v1.1.0`, et faire scraper l'application par Prometheus via un ServiceMonitor.

### Incident préalable

Au démarrage de la session, VS Code n'arrivait plus à joindre WSL :

```
Failed to probe if server is already installed: code: 4294967295, Défaillance irrémédiable
Code d'erreur: Wsl/Service/E_UNEXPECTED
WSL Daemon exited with code 0
```

Le service WSL lui-même était en échec, pas la distribution. Résolution : fermer VS Code, quitter Docker Desktop, puis depuis PowerShell :

```powershell
wsl --shutdown
wsl -l -v
```

Puis relancer Docker Desktop et le cluster :

```bash
minikube start -p observability
```

Les deux nœuds ont redémarré avec leurs conteneurs existants. Tous les pods sont revenus `Running` en deux minutes, avec un `RESTARTS` incrémenté.

### Lecture du code existant

`http_requests_total` est déclarée avec `promauto.NewCounterVec`, qui enregistre automatiquement la métrique dans le registre par défaut, et porte les labels `handler`, `method`, `code`. Elle est incrémentée dans le `defer` du middleware, donc après l'exécution du handler, quand le code de réponse est connu.

Le middleware utilise `recover()` parce que les handlers du guestbook font `panic()` quand Redis est indisponible, via la fonction `HandleError`. Sans récupération, le panic remonterait jusqu'à Negroni, la métrique ne serait jamais incrémentée et la ligne de log jamais écrite : les erreurs les plus intéressantes seraient précisément celles qu'on ne verrait pas.

Le commentaire du code précise pourquoi ce `recover()` est fait ici plutôt que par le middleware Recovery de Negroni : la stack trace multi-lignes de ce dernier casserait le format une-ligne-un-objet-JSON attendu par Loki.

### Modification de guestbook/metrics.go

Déclaration ajoutée à la place du `// TODO (exercise 3)` :

```go
var httpRequestDuration = promauto.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "HTTP request latency in seconds, by route and method.",
		Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.2, 0.4, 0.8, 1.6, 3.2},
	},
	[]string{"handler", "method"},
)
```

Observation ajoutée dans le `defer` du middleware, après le calcul de `duration` :

```go
httpRequestDuration.WithLabelValues(handler, req.Method).Observe(duration.Seconds())
```

Le `git diff` complet tient en deux hunks, c'est la seule modification du code de tout le TP.

### Build et déploiement

```bash
docker build -t guestbook:v1.1.0 guestbook/
minikube -p observability image load guestbook:v1.1.0
minikube -p observability image ls | grep guestbook
```

```bash
sed -i 's/tag: v1.0.0/tag: v1.1.0/' deploy/guestbook/values.yaml
helm upgrade guestbook deploy/guestbook -n guestbook
kubectl get pods -n guestbook -w
```

Rolling update : les trois pods sont remplacés un par un, le nouveau devenant `Ready` avant que l'ancien ne soit terminé.

### Vérification de la métrique

```bash
kubectl port-forward -n guestbook svc/guestbook 3000:3000
curl -s localhost:3000/lrange/guestbook > /dev/null
curl -s localhost:3000/metrics | grep http_request_duration_seconds_bucket | head -12
```

```
http_request_duration_seconds_bucket{handler="/healthz",method="GET",le="0.01"} 70
http_request_duration_seconds_bucket{handler="/healthz",method="GET",le="0.025"} 71
http_request_duration_seconds_bucket{handler="/healthz",method="GET",le="0.05"} 71
...
http_request_duration_seconds_bucket{handler="/healthz",method="GET",le="+Inf"} 71
```

Les buckets sont cumulatifs : 70 requêtes sous 10 ms, 71 sous 25 ms et à toutes les bornes supérieures. Une seule requête se situe donc entre 10 et 25 ms, et aucune ne dépasse 25 ms.

### ServiceMonitor

`exercise-3/servicemonitor.yaml` :

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: guestbook
  namespace: guestbook
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: guestbook
      app.kubernetes.io/instance: guestbook
  endpoints:
    - port: http
      path: /metrics
      interval: 15s
```

```bash
kubectl apply -f exercise-3/servicemonitor.yaml
kubectl get servicemonitor -n guestbook
```

Le `selector` reprend les labels générés par `_helpers.tpl`, les mêmes que ceux du Service.

Le `port: http` désigne le nom du port dans le Service, pas son numéro. C'est la raison pour laquelle le chart le nomme explicitement.

L'intervalle de 15 secondes prépare l'exercice 4 : une fenêtre de `rate()` doit couvrir au moins quatre intervalles de scrape, donc 1 minute suffira.

### Résultat dans Prometheus

Page `Status > Target health`, filtre `guestbook` :

```
serviceMonitor/guestbook/guestbook/0     3 / 3 up
```

Trois endpoints, un par pod. Prometheus interroge chaque pod séparément, contrairement au port-forward sur le Service qui n'en touche qu'un seul.

### Vérifications

- [x] `/metrics` expose `http_request_duration_seconds_bucket`
- [x] La target du guestbook est `UP` dans Prometheus, avec une entrée par pod
- [x] Je sais expliquer la différence entre un counter, une gauge et un histogramme

---

# Apprentissages

## Exercice 3 : instrumenter le guestbook

Ajouter la mesure de latence qui manquait à l'application, et faire remonter ses métriques dans Prometheus.

### Problème rencontré et pourquoi il est survenu

La difficulté n'était pas d'écrire le code mais de choisir les bornes des buckets, et ce choix n'est pas réversible facilement.

Un histogramme Prometheus ne stocke pas les valeurs observées. Il ne garde que des compteurs cumulés par intervalle : combien d'observations sous 10 ms, combien sous 25 ms, et ainsi de suite. Quand on demande un percentile avec `histogram_quantile()`, la fonction repère le bucket où tombe ce percentile et interpole **linéairement** à l'intérieur.

Le SLO de latence du guestbook est : 95 % des lectures et écritures en moins de 400 ms. Avec des bornes mal placées, par exemple 0,25 puis 1 seconde, un p95 réel à 400 ms serait estimé quelque part entre les deux par interpolation, avec une erreur potentielle de plusieurs centaines de millisecondes. Impossible alors d'affirmer si le SLO est respecté ou non.

Le second piège est la cardinalité. Chaque borne crée une série temporelle pour chaque combinaison de labels. Multiplier les bornes pour gagner en précision fait exploser le nombre de séries, et donc la mémoire de Prometheus.

### Solution appliquée et pourquoi cette solution fonctionne

Des bornes resserrées dans la zone du trafic normal, une borne à exactement `0.4` qui est le seuil du SLO, et des bornes plus espacées au-dessus pour capter les requêtes lentes sans coût inutile.

La borne à 0,4 est ce qui permet de mesurer le SLO sans interpolation hasardeuse. Les bornes supérieures doivent exister malgré tout : sans elles, toutes les requêtes lentes tomberaient dans `+Inf` et on ne saurait pas distinguer une requête à 500 ms d'une requête à 10 secondes.

L'histogramme porte `handler` et `method`, mais pas `code`, contrairement au counter. La latence d'une réponse en erreur n'apprend pas grand-chose, et ajouter ce troisième label multiplierait le nombre de séries par le nombre de codes HTTP rencontrés.

Pour le scrape, le ServiceMonitor sélectionne le Service par ses labels et le port par son nom. Il a été pris en compte immédiatement grâce à `serviceMonitorSelectorNilUsesHelmValues: false`, posé à l'exercice 2.

### Ce que j'ai appris

La différence entre les trois types de métriques, qui n'est pas qu'une question de vocabulaire.

Un **counter** ne fait que croître, et sa valeur brute n'a aucun intérêt : savoir qu'il y a eu 4 217 requêtes depuis le démarrage du pod ne dit rien. On lui applique `rate()` pour obtenir un débit. Il redémarre à zéro quand le pod redémarre, et `rate()` sait détecter ces remises à zéro.

Une **gauge** monte et descend, et sa valeur instantanée a du sens. `guestbook_redis_up` vaut 1 ou 0, et c'est exactement l'information utile.

Un **histogramme** répartit des observations dans des intervalles et permet de calculer des percentiles après coup, côté serveur. C'est ce qui le distingue d'une moyenne : une moyenne de 100 ms peut cacher 95 % de requêtes à 50 ms et 5 % à 1 seconde. Ce sont ces 5 % qui font partir les utilisateurs, et seul un percentile les montre.

Que les bornes doivent être choisies en fonction du SLO qu'on veut mesurer, et non l'inverse. Instrumenter d'abord puis définir le SLO ensuite donne des métriques inutilisables pour le vérifier.

Et qu'un ServiceMonitor crée une target par pod, pas par Service. C'est ce qui permettra à l'exercice 4 de repérer qu'un seul pod est lent, information qu'un port-forward sur le Service masquerait complètement.