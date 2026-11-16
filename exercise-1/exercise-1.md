# Exercice 1 : Topology Spread Constraints

## Doc

### Objectif

Forcer la répartition des pods du guestbook sur les deux nœuds, puis vérifier le comportement lors d'une panne de nœud simulée par `kubectl drain`.

### Répartition initiale

```bash
kubectl get pods -n guestbook -o wide
```

```
guestbook-747b5fc8c7-nrqxt   observability-m02
guestbook-747b5fc8c7-p8v88   observability
guestbook-747b5fc8c7-sg794   observability-m02
```

Deux pods sur un nœud, un sur l'autre. Le résultat est correct mais rien ne le garantissait : le scheduler place où il y a de la place, il aurait pu mettre les trois au même endroit.

### Modifications apportées au chart

Ajout dans `deploy/guestbook/values.yaml` :

```yaml
topologySpread:
  maxSkew: 1
  whenUnsatisfiable: DoNotSchedule
```

Ajout dans `deploy/guestbook/templates/deployment.yaml`, dans le `spec` du template de pod, juste avant `containers` :

```yaml
      topologySpreadConstraints:
        - maxSkew: {{ .Values.topologySpread.maxSkew }}
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: {{ .Values.topologySpread.whenUnsatisfiable }}
          labelSelector:
            matchLabels:
              {{- include "guestbook.labels" . | nindent 14 }}
```

Le `labelSelector` reprend le helper `guestbook.labels`, c'est-à-dire les mêmes labels que ceux du Deployment et du Service.

### Erreur de chemin

Première tentative lancée depuis le mauvais répertoire :

```
aboukhedcha@LENOVOANAS:.../devops-class-4/guestbook$ helm upgrade guestbook deploy/guestbook -n guestbook
Error: repo deploy not found
```

Le dépôt contient deux répertoires nommés `guestbook` :

- `guestbook/` à la racine : le code Go
- `deploy/guestbook/` : le chart Helm

Les fichiers avaient été créés dans le premier. Et depuis ce répertoire, `deploy/guestbook` n'existe pas, donc Helm a interprété `deploy` comme le nom d'un dépôt de charts distant.

### Deuxième symptôme : upgrade sans effet

Après correction du chemin, l'upgrade réussit mais rien ne bouge :

```
Release "guestbook" has been upgraded. REVISION: 2
guestbook-747b5fc8c7-nrqxt   1/1   Running   0   17m
guestbook-747b5fc8c7-p8v88   1/1   Running   0   17m
guestbook-747b5fc8c7-sg794   1/1   Running   0   17m
```

Mêmes noms de pods, 17 minutes d'âge : aucun rolling update. Un `git diff` vide a confirmé que les fichiers n'avaient pas été enregistrés.

Les modifications ont été refaites en ligne de commande, puis vérifiées :

```bash
git --no-pager diff
helm template guestbook deploy/guestbook | grep -B 2 -A 8 topologySpread
```

Rendu obtenu :

```yaml
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: DoNotSchedule
          labelSelector:
            matchLabels:
              app.kubernetes.io/name: guestbook
              app.kubernetes.io/instance: guestbook
```

### Déploiement

```bash
helm upgrade guestbook deploy/guestbook -n guestbook
kubectl get pods -n guestbook -o wide
```

Nouveau ReplicaSet, nouveaux noms de pods, répartition finale 2 sur `observability` et 1 sur `observability-m02`.

### Drain du nœud

Localisation de Redis avant l'opération :

```bash
kubectl get pods -n data -o wide
```

Redis était sur `observability-m02`, avec un seul replica.

```bash
kubectl drain observability-m02 --ignore-daemonsets --delete-emptydir-data
```

```
node/observability-m02 cordoned
Warning: ignoring DaemonSet-managed Pods: kube-system/kindnet-9lzh4, kube-system/kube-proxy-mbkwl
evicting pod guestbook/guestbook-87896d58d-xbbnz
evicting pod data/redis-6984b97b6c-c7vlg
node/observability-m02 drained
```

Le drain a porté sur le nœud worker plutôt que sur `observability`, qui héberge le control plane : drainer ce dernier sous minikube évince aussi des composants du cluster et réclame `--force`, ce qui aurait brouillé l'observation.

### Observations pendant le drain

```
NAME                        READY   STATUS    NODE
guestbook-87896d58d-c75r5   0/1     Pending   <none>
guestbook-87896d58d-h6wnt   0/1     Running   observability
guestbook-87896d58d-rpzm7   0/1     Running   observability
```

Trois choses se sont produites simultanément.

**Le pod évincé reste en `Pending`.** Le `describe` donne la raison :

```
Topology Spread Constraints:  kubernetes.io/hostname:DoNotSchedule when max skew 1 is exceeded
FailedScheduling: 0/2 nodes are available: 1 node(s) didn't match pod topology spread constraints,
1 node(s) were unschedulable.
```

Un nœud est cordonné, l'autre porte déjà 2 pods. Le placer là porterait l'écart à 3 contre 0, au-delà du `maxSkew: 1`. Aucun placement possible.

**Redis a été évincé et recréé sur `observability`.** Son PVC est resté `Bound` au même volume.

**Les deux pods survivants sont passés `0/1`.** Running mais pas Ready : leur readiness probe interroge `/healthz`, qui ping Redis, lequel était en cours de migration. L'application était donc indisponible alors que deux pods tournaient.

### Remise en service

```bash
kubectl uncordon observability-m02
kubectl get pods -n guestbook -o wide
```

Le pod `Pending` s'est placé sur `observability-m02` immédiatement. Les deux autres n'ont pas bougé, et Redis non plus.

Réponse à la question du README : les pods ne se rééquilibrent pas tout seuls.

### Vérifications

- [x] Mes pods sont répartis sur les 2 nœuds
- [ ] Le guestbook reste accessible pendant le drain d'un nœud : non, voir la fiche
- [x] Je sais expliquer la différence entre `DoNotSchedule` et `ScheduleAnyway`

---

# Apprentissages

## Exercice 1 : Topology Spread Constraints

Répartir les replicas du guestbook sur les deux nœuds du cluster, et vérifier par un `drain` que l'application survit à la perte d'un nœud.

### Problème rencontré et pourquoi il est survenu

L'application n'a pas survécu au drain, mais pas pour la raison attendue.

Les Topology Spread Constraints ont fonctionné : les pods étaient bien répartis 2/1, et deux d'entre eux ont continué de tourner pendant toute l'opération. Pourtant le guestbook est devenu injoignable, les deux pods survivants passant `0/1`.

La cause est Redis. Il n'a qu'un seul replica, il se trouvait sur le nœud drainé, et il a donc été évincé puis recréé ailleurs. Pendant cette migration, la readiness probe du guestbook, qui interroge `/healthz`, lequel ping Redis, a échoué sur tous les pods. Le Service les a retirés du load balancing et l'application n'a plus répondu.

Un second effet, attendu celui-là : le pod évincé est resté en `Pending`, le scheduler ne trouvant aucun placement compatible avec `maxSkew: 1` alors qu'un nœud était cordonné.

### Solution appliquée et pourquoi cette solution fonctionne

Pour la répartition, une contrainte sur `kubernetes.io/hostname` avec `maxSkew: 1`, dont le `labelSelector` reprend les labels du chart. Le scheduler compte les pods correspondants par nœud et refuse tout placement qui creuserait l'écart au-delà de 1.

Pour l'indisponibilité, il n'y a rien à corriger dans le guestbook : le problème est en amont. Redis en Deployment à un replica, avec un volume lié à un nœud, est un point unique de défaillance. Le rendre résistant demanderait une réplication Redis, ce qui dépasse le cadre de l'exercice mais explique pourquoi les bases de données sont habituellement déployées en StatefulSet avec plusieurs instances.

Le pod `Pending`, lui, n'appelle pas de correction non plus : c'est le comportement demandé. `DoNotSchedule` préfère un pod non démarré à une répartition dégradée.

### Ce que j'ai appris

Que répartir les replicas d'une application ne la rend pas disponible si elle dépend d'un composant unique. La chaîne complète compte, pas seulement la couche qu'on a soignée. C'est exactement ce que mesure un SLO vu de l'utilisateur, et ce que des métriques par pod ne montreraient pas.

Que `DoNotSchedule` et `ScheduleAnyway` arbitrent entre deux risques. Le premier est une contrainte dure : le pod reste `Pending` si elle ne peut être respectée, ce qui préserve la répartition au prix d'une capacité réduite. Le second est une préférence : le scheduler fait au mieux puis place le pod quand même, ce qui donne une application debout mais potentiellement entièrement sur un seul nœud. Ici `DoNotSchedule` a coûté un pod pendant tout le drain ; `ScheduleAnyway` aurait donné trois pods sur `observability`, donc aucune protection contre la panne suivante.

Que les contraintes sont évaluées au moment du scheduling et jamais ensuite. Après le `uncordon`, seul le pod `Pending` s'est placé : les deux déjà démarrés sont restés où ils étaient. Rien dans Kubernetes ne déplace un pod qui tourne. Le rééquilibrage arrive au prochain rolling update, ou demande un composant dédié comme le descheduler.

Et, côté méthode, qu'un `helm upgrade` qui renvoie une nouvelle révision ne prouve rien. Tant que les noms de pods et leur âge ne changent pas, le template n'a pas bougé. `git diff` et `helm template` avant l'upgrade évitent de chercher longtemps pourquoi une contrainte ne s'applique pas.