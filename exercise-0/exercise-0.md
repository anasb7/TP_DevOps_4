# Exercice 0 : préparer l'environnement

## Doc

### Objectif

Créer un cluster minikube de 2 nœuds, construire le guestbook `v1.0.0` et le déployer avec Redis.

### Vérification des ressources

Le cluster demande 6 vCPU et 8 Go, auxquels s'ajouteront Prometheus, Grafana et Chaos Mesh.

```bash
free -h
nproc
```

Résultat : 15 Gi de RAM dont 13 Gi disponibles, 16 cœurs. Largement suffisant.

Libération du cluster du Jour 3 :

```bash
minikube stop
```

### Création du cluster

```bash
minikube start -p observability --nodes 2 --cpus 3 --memory 4096
minikube -p observability addons enable metrics-server
minikube -p observability addons enable ingress
kubectl wait -n ingress-nginx --for=condition=Ready pod -l app.kubernetes.io/component=controller --timeout=5m
```

Deux conteneurs Docker créés, `observability` (control plane) et `observability-m02` (worker). Les deux addons sont activés et l'Ingress controller passe `Ready`.

### Construction et chargement de l'image

```bash
docker build -t guestbook:v1.0.0 guestbook/
minikube -p observability image load guestbook:v1.0.0
minikube -p observability image ls | grep guestbook
```

Le Dockerfile est multi-étapes : build sur `golang:1.27-alpine` avec `CGO_ENABLED=0`, puis copie du binaire et de `public/` dans l'image finale.

Le chargement dans minikube est indispensable : `values.yaml` fixe `pullPolicy: Never`, donc Kubernetes n'ira jamais chercher l'image sur Docker Hub. Si elle n'est pas présente localement, le pod reste en `ErrImageNeverPull`.

### Déploiement

```bash
kubectl apply -f deploy/namespaces.yaml
kubectl apply -n data -f deploy/redis/
helm install guestbook deploy/guestbook -n guestbook
kubectl get pods -n guestbook -o wide
kubectl get pods -n data -o wide
kubectl get nodes
```

Répartition initiale des 3 pods : 2 sur `observability-m02`, 1 sur `observability`. Redis sur `observability-m02`.

### Vérification des métriques

```bash
kubectl port-forward -n guestbook svc/guestbook 3000:3000
```

Dans un second terminal :

```bash
curl -s localhost:3000/healthz -w "\n%{http_code}\n"
curl -s localhost:3000/metrics | grep -E "^(http_requests_total|guestbook_redis_up)"
```

Sortie :

```
200
guestbook_redis_up 1
http_requests_total{code="200",handler="/healthz",method="GET"} 30
http_requests_total{code="200",handler="/lrange/{key}",method="GET"} 17
http_requests_total{code="200",handler="static",method="GET"} 4
http_requests_total{code="500",handler="/healthz",method="GET"} 3
```

Les trois `code="500"` datent des premières secondes, avant que Redis ne soit joignable.

À noter : ces métriques ne viennent que d'un seul pod. Le port-forward vise un pod précis, pas les trois. C'est ce que Prometheus résoudra à l'exercice 3 en scrapant chaque pod séparément.

### Lecture de guestbook/metrics.go

**Deux métriques exposées :**

- `http_requests_total`, un counter avec les labels `handler`, `method`, `code`
- `guestbook_redis_up`, une gauge valant 1 ou 0, mise à jour par le handler `/healthz`

Un commentaire marque l'emplacement de la troisième, à écrire à l'exercice 3 :

```go
// TODO (exercise 3): http_request_duration_seconds
```

**Champs des logs JSON :** `version`, `method`, `handler`, `path`, `code`, `duration_ms`, `error`, `trace_id`, plus un niveau déduit du code HTTP par `logLevel()` : `ERROR` au-delà de 500, `WARN` au-delà de 400, `INFO` sinon.

**Deux choix de conception à relever :**

`routeName()` renvoie le template de route (`/lrange/{key}`) et jamais l'URL réelle. Avec l'URL, chaque message créerait sa propre série temporelle.

Le middleware fait son propre `recover()` au lieu d'utiliser celui de Negroni. La raison est dans le commentaire du code : la stack trace multi-lignes de Negroni casserait le format une-ligne-un-objet-JSON attendu par Loki.

### Vérifications

- [x] `kubectl get nodes` affiche 2 nœuds `Ready`
- [x] Le guestbook `v1.0.0` fonctionne et est connecté à Redis
- [x] `curl` sur `/metrics` retourne des métriques au format Prometheus
- [x] Je sais lire `guestbook/metrics.go`

---

# Apprentissages

## Exercice 0 : préparer l'environnement

Monter un cluster de 2 nœuds et y déployer le guestbook `v1.0.0`, celui qui expose ses propres métriques.

### Problème rencontré et pourquoi il est survenu

Les métriques du guestbook montrent trois réponses 500 sur `/healthz` :

```
http_requests_total{code="500",handler="/healthz",method="GET"} 3
```

Les pods du guestbook ont démarré avant que Redis soit joignable. Le handler `/healthz` ping Redis, échoue, met la gauge `guestbook_redis_up` à 0 et renvoie 500.

Aucune de ces erreurs n'a atteint un utilisateur : la readiness probe interroge justement `/healthz`, donc le Service a tenu les pods hors du load balancing tant qu'ils répondaient 500.

### Solution appliquée et pourquoi cette solution fonctionne

Rien à corriger, le comportement est celui attendu. Le chart sépare correctement les deux probes :

- readiness sur `/healthz`, qui dépend de Redis : pas de Redis, pas de trafic
- liveness en `tcpSocket` sur le port 3000, qui n'en dépend pas : pas de redémarrage en boucle si Redis tombe

Si la liveness avait pointé sur `/healthz`, une panne de Redis aurait fait redémarrer tous les pods du guestbook, alors qu'ils fonctionnent parfaitement.

### Ce que j'ai appris

Que la distinction entre liveness et readiness n'est pas une subtilité de configuration mais une décision de conception : l'une répond « ce pod peut-il servir maintenant », l'autre « ce pod est-il cassé au point qu'il faille le tuer ». Les confondre transforme une panne de dépendance en panne généralisée.

Que des erreurs dans les métriques ne signifient pas forcément des erreurs vues par les utilisateurs. Il faudra s'en souvenir en construisant le dashboard RED : les appels des probes et des scrapes ne sont pas du trafic utilisateur.

Que `pullPolicy: Never` et `minikube image load` vont ensemble. Le cluster a son propre stockage d'images, indépendant du Docker local, et sans ce chargement explicite le pod ne démarrerait jamais.