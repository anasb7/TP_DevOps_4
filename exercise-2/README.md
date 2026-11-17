# Exercice 2 : déployer la stack Prometheus

## Doc

### Objectif

Installer `kube-prometheus-stack` dans un namespace `monitoring`, obtenir des targets saines et explorer les dashboards Kubernetes fournis.

### Ajout du dépôt Helm

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
```

### Fichier de valeurs

`exercise-2/monitoring.yaml`, dans sa version initiale :

```yaml
prometheus:
  prometheusSpec:
    serviceMonitorSelectorNilUsesHelmValues: false
    ruleSelectorNilUsesHelmValues: false
    retention: 7d
    resources:
      requests:
        cpu: 100m
        memory: 512Mi

grafana:
  adminPassword: admin
```

Les deux premières valeurs sont celles que le README signale pour les exercices 3 et 5. Par défaut, l'opérateur ne fait sélectionner à Prometheus que les ServiceMonitors et les PrometheusRules portant le label du chart. En les passant à `false`, Prometheus prend en compte toutes les ressources de ce type présentes dans le cluster, ce qui permettra au ServiceMonitor de l'exercice 3 et aux règles de l'exercice 5 d'être pris en compte sans bricolage de labels.

### Installation

```bash
mkdir -p exercise-2
helm install monitoring prometheus-community/kube-prometheus-stack \
  -n monitoring --create-namespace \
  -f exercise-2/monitoring.yaml
kubectl get pods -n monitoring -w
```

Sept pods démarrent : l'opérateur, Prometheus, Alertmanager, Grafana, kube-state-metrics et un node-exporter par nœud. Tout est `Running` en moins de deux minutes.

### Accès aux interfaces

```bash
kubectl port-forward -n monitoring svc/monitoring-kube-prometheus-prometheus 9090:9090
kubectl port-forward -n monitoring svc/monitoring-grafana 3001:80
```

Mot de passe Grafana, tel que le README invite à le retrouver dans le Secret :

```bash
kubectl get secret -n monitoring monitoring-grafana -o jsonpath="{.data.admin-password}" | base64 -d; echo
```

Les CRDs installées par l'opérateur :

```bash
kubectl get crd | grep monitoring.coreos.com
```

### Targets DOWN

Page `Status > Target health`, filtre `DOWN`. Trois targets en échec :

```
serviceMonitor/monitoring/monitoring-kube-prometheus-kube-controller-manager/0   0/1 up
serviceMonitor/monitoring/monitoring-kube-prometheus-kube-etcd/0                 0/1 up
serviceMonitor/monitoring/monitoring-kube-prometheus-kube-scheduler/0            0/1 up
```

Messages d'erreur, identiques dans leur nature :

```
Get "https://192.168.58.2:10257/metrics": dial tcp 192.168.58.2:10257: connect: connection refused
Get "http://192.168.58.2:2381/metrics": dial tcp 192.168.58.2:2381: connect: connection refused
Get "https://192.168.58.2:10259/metrics": dial tcp 192.168.58.2:10259: connect: connection refused
```

`192.168.58.2` est l'IP du nœud `observability`. Les ports correspondent au controller manager (10257), à etcd (2381) et au scheduler (10259).

`kube-proxy`, lui, est `UP` : il n'a pas été désactivé.

### Correction

Ajout dans `exercise-2/monitoring.yaml` :

```yaml
kubeControllerManager:
  enabled: false
kubeScheduler:
  enabled: false
kubeEtcd:
  enabled: false
```

```bash
helm upgrade monitoring prometheus-community/kube-prometheus-stack \
  -n monitoring \
  -f exercise-2/monitoring.yaml
```

Après rechargement, la page `Status > Target health` filtrée sur `DOWN` est vide.

### Exploration

Dashboards Grafana parcourus :

- *Kubernetes / Compute Resources / Namespace (Pods)*, namespace `guestbook` : consommation CPU et mémoire des trois pods
- *Kubernetes / Compute Resources / Node (Pods)* : vue par nœud

Requêtes PromQL testées dans Prometheus :

```
up
```

Une série par target, valeur 1 si le dernier scrape a réussi.

```
node_memory_MemAvailable_bytes
```

Deux séries, une par nœud.

```
sum(rate(container_cpu_usage_seconds_total{namespace="guestbook"}[5m])) by (pod)
```

Consommation CPU par pod du guestbook, qui resservira à l'exercice 4.

### Vérifications

- [x] Les targets Prometheus sont `UP`, et je sais expliquer celles que j'ai désactivées
- [x] Grafana affiche les dashboards Kubernetes
- [x] Je sais retrouver la consommation CPU et mémoire des pods du guestbook

---

# Apprentissages

## Exercice 2 : déployer la stack Prometheus

Installer Prometheus, Alertmanager et Grafana en un seul chart, et obtenir un cluster entièrement scrapé.

### Problème rencontré et pourquoi il est survenu

Trois targets restaient `DOWN` juste après l'installation : `kube-controller-manager`, `kube-etcd` et `kube-scheduler`. Toutes avec la même erreur, `connect: connection refused` sur l'IP du nœud.

Le refus de connexion est un diagnostic précis : ce n'est ni un problème d'authentification, ni un certificat invalide, ni un pare-feu qui jette les paquets en silence. Quelque chose a répondu, pour dire que rien n'écoutait sur ce port.

C'est le cas sous minikube : ces trois composants sont configurés pour n'écouter que sur `127.0.0.1` à l'intérieur du nœud, parce que leurs endpoints exposent des informations sensibles sur le cluster. Le chart, lui, crée des Services pointant vers l'IP du nœud, en supposant une configuration de production où ces composants écoutent sur l'interface réseau.

Prometheus tourne dans un pod, avec sa propre pile réseau : son `localhost` n'est pas celui du nœud. Il ne peut donc pas les joindre.

### Solution appliquée et pourquoi cette solution fonctionne

Désactivation des trois dans les valeurs du chart, ce qui supprime les Services et les ServiceMonitors correspondants. Prometheus n'a plus de target à tenter, la page `DOWN` est vide.

`kube-proxy` n'a pas été touché puisqu'il fonctionnait. Désactiver par précaution ce qui marche reviendrait à perdre une source de métriques sans raison.

Ce n'est pas une correction à proprement parler, c'est une adaptation au terrain. Sur un vrai cluster, ces trois composants sont joignables et il faut les surveiller : une latence d'écriture etcd qui monte ou un scheduler saturé sont des signaux importants. Ici, sur un cluster jetable, il n'y aurait rien à faire de l'information.

### Ce que j'ai appris

Qu'une target `DOWN` n'est pas forcément un problème à corriger. La bonne question est de savoir si cette métrique me serait utile, et si je peux agir dessus. Garder des targets rouges en permanence est pire que de les désactiver : on s'habitue au rouge, et on ne voit plus la vraie panne quand elle arrive.

Que le message d'erreur oriente le diagnostic plus que le nom du composant. `connection refused` écarte d'emblée toutes les hypothèses d'authentification et de TLS, et pointe vers un service absent de l'adresse interrogée.

Que `serviceMonitorSelectorNilUsesHelmValues` et `ruleSelectorNilUsesHelmValues` conditionnent toute la suite du TP. L'opérateur Prometheus ne découvre pas automatiquement les ressources du cluster : il applique un sélecteur de labels, dont le comportement par défaut est restrictif. Sans ces deux valeurs, le ServiceMonitor de l'exercice 3 serait créé, visible avec `kubectl get servicemonitor`, et purement et simplement ignoré par Prometheus.