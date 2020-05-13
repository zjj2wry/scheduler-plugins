# Overview
in some specific scenarios, statefulset hopes to be able to schedule to the same node. this plugin implement statafulset stable schedule.

# demo
1. use kind to build a multi-worker k8s cluster
```yaml
kind: Cluster
apiVersion: kind.sigs.k8s.io/v1alpha3
nodes:
- role: control-plane
- role: worker
- role: worker
```

```bash
kind create cluster --config=cluster.yaml
```

2. use multi profile of schedule and add plugin configuration
```yaml
apiVersion: kubescheduler.config.k8s.io/v1alpha2
kind: KubeSchedulerConfiguration
leaderElection:
  leaderElect: false
profiles:
  - schedulerName: statefulset-stable
    plugins:
      preFilter:
        enabled:
          - name: statefulset-stable
      postBind:
        enabled:
          - name: statefulset-stable
```

3. create a statefulset
```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: web
spec:
  serviceName: "nginx"
  replicas: 2
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
        statefulset-stable.scheduling.sigs.k8s.io: "true"
    spec:
      containers:
        - name: nginx
          image: nginx:latest
          ports:
            - containerPort: 80
              name: web
      schedulerName: statefulset-stable
```
the statefulset annotation records the scheduled records. as follows, web-0 will alway schedule to kind-worker when rescheduling
```yaml
annotations:
    statefulset-stable.scheduling.sigs.k8s.io/record: '{"Records":{"web-0":"kind-worker","web-1":"kind-worker2"}}'
```
