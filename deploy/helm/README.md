# Helm deployment

`deploy/helm/app` is a single reusable chart. Deploy every backend service by changing values only:

```bash
helm upgrade --install gateway deploy/helm/app -f deploy/helm/gateway-values.example.yaml
helm upgrade --install reservation deploy/helm/app -f reservation-values.yaml
helm upgrade --install payment deploy/helm/app -f payment-values.yaml
helm upgrade --install loyalty deploy/helm/app -f loyalty-values.yaml
helm upgrade --install identity deploy/helm/app -f identity-values.yaml
helm upgrade --install statistics deploy/helm/app -f statistics-values.yaml
helm upgrade --install ui deploy/helm/app -f ui-values.yaml
```

Before deploying, replace image repositories, public hosts, database URLs, and credentials in values files or CI secrets.
