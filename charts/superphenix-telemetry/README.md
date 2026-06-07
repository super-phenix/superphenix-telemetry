# superphenix-telemetry

A Helm chart to deploy the Superphenix Telemetry ingest server

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: latest](https://img.shields.io/badge/AppVersion-latest-informational?style=flat-square)

## Installing the Chart

```sh
helm install superphenix-telemetry oci://ghcr.io/super-phenix/helm-charts/superphenix-telemetry \
  --namespace superphenix-telemetry --create-namespace
```

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity rules for pod placement. |
| autoscaling.enabled | bool | `false` | Enable a HorizontalPodAutoscaler for the deployment. |
| autoscaling.maxReplicas | int | `10` | Maximum replica count when autoscaling is enabled. |
| autoscaling.minReplicas | int | `1` | Minimum replica count when autoscaling is enabled. |
| autoscaling.targetCPUUtilizationPercentage | int | `80` | Target average CPU utilisation percentage. |
| config.listenAddr | string | `":8080"` | Address and port the HTTP server binds to. |
| config.logLevel | string | `"info"` | Log level: debug, info, warn, error. |
| config.rateLimit | object | `{"max":10,"window":"1h"}` | Maximum requests permitted from a single client identifier within rateLimit.window before the server returns 429. |
| config.timeouts | object | `{"idle":"60s","readHeader":"5s","shutdown":"10s","write":"10s"}` | Per-request timeouts on the HTTP server. |
| config.trustForwardedFor | bool | `false` | Honour the X-Forwarded-For header when extracting the client IP. Enable this only when the pod sits behind a reverse proxy you control; otherwise any client can spoof the header to evade rate limiting. |
| fullnameOverride | string | `""` | Override the fully qualified resource name. |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy. |
| image.repository | string | `"ghcr.io/super-phenix/superphenix-telemetry"` | Container image repository. |
| image.tag | string | `""` | Image tag. Defaults to the chart appVersion when empty. |
| imagePullSecrets | list | `[]` | Image pull secrets for private registries. |
| ingress.annotations | object | `{}` | Annotations applied to the Ingress. |
| ingress.className | string | `""` | Ingress class name. |
| ingress.enabled | bool | `false` | Enable the Ingress resource. The chart does not assume an IngressClass; set ingress.className to match your environment. |
| ingress.hosts | list | `[{"host":"telemetry.example.com","paths":[{"path":"/","pathType":"Prefix"}]}]` | Host rules for the Ingress. |
| ingress.tls | list | `[]` | TLS configuration for the Ingress. |
| nameOverride | string | `""` | Override the chart name used in resource names. |
| nodeSelector | object | `{}` | Node selector for pod placement. |
| podAnnotations | object | `{}` | Annotations applied to the Pod. |
| podLabels | object | `{}` | Labels applied to the Pod. |
| podSecurityContext | object | `{"runAsGroup":1000,"runAsNonRoot":true,"runAsUser":1000,"seccompProfile":{"type":"RuntimeDefault"}}` | Pod-level securityContext applied to the entire Pod. |
| replicaCount | int | `1` | Number of replicas. Because the rate limiter and IP-salt are in-memory per pod, scaling beyond 1 replica means each pod tracks its own salt and quota state independently - a client that gets throttled on one pod can still be served by another. Keep at 1 unless you have a sticky-session ingress. |
| resources | object | `{"limits":{"cpu":"500m","memory":"256Mi"},"requests":{"cpu":"50m","memory":"64Mi"}}` | Resource requests and limits for the container. |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true,"runAsNonRoot":true,"runAsUser":1000}` | Container-level securityContext. Defaults follow the restricted Pod Security Standard. |
| service.port | int | `8080` | Service port that fronts the container's HTTP port. |
| service.type | string | `"ClusterIP"` | Kubernetes Service type. |
| serviceAccount.annotations | object | `{}` | Annotations applied to the ServiceAccount. |
| serviceAccount.automount | bool | `false` | Mount the ServiceAccount token into the pod. |
| serviceAccount.create | bool | `true` | Create a dedicated ServiceAccount for the pod. |
| serviceAccount.name | string | `""` | ServiceAccount name. Generated from the chart fullname if empty. |
| serviceMonitor.enabled | bool | `false` | Create a Prometheus Operator ServiceMonitor for /metrics. Requires the monitoring.coreos.com CRDs to be installed in the cluster. |
| serviceMonitor.interval | string | `"30s"` | Scrape interval applied by the ServiceMonitor. |
| serviceMonitor.labels | object | `{}` | Labels added to the ServiceMonitor so Prometheus can select it. |
| tolerations | list | `[]` | Tolerations for pod placement. |

---

----------------------------------------------
Autogenerated from chart metadata using [helm-docs v1.14.2](https://github.com/norwoodj/helm-docs/releases/v1.14.2)
