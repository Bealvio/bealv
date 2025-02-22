# Main Cluster

This repo only contains working workload.

Most of the system and infra workload is deployed through automation.

## How to add workload

Create a kustomization fluxcd file that point to `gitops/apps/<workloadname>` and put it in `gitops/kustomizations`.

You workload will be deployed.

## Expose services

### To expose internally

Create an Ingress resource:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: test
  annotations:
    cert-manager.io/cluster-issuer: bealv
  labels:
    probe: enabled
spec:
  rules:
    - host: 'test.bealv.lan'
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: test
                port:
                  number: 9001
  tls:
    - hosts:
        - 'test.bealv.lan'
      secretName: test-tls
```

### To expose externally

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: HTTPRoute
metadata:
  name: test
spec:
  parentRefs:
    - name: public
      namespace: istio-system
      sectionName: https
  hostnames:
    - test.bealv.io
  rules:
    - backendRefs:
        - name: test
          port: 8096
```
