#!/usr/bin/env bash
set -euo pipefail

kubectl create namespace "$NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

helm repo add jetstack https://charts.jetstack.io
helm repo update
helm upgrade --install cert-manager jetstack/cert-manager \
  --namespace cert-manager \
  --create-namespace \
  --version v1.16.2 \
  --set crds.enabled=true

kubectl -n cert-manager rollout status deploy/cert-manager --timeout=5m
kubectl -n cert-manager rollout status deploy/cert-manager-webhook --timeout=5m
kubectl -n cert-manager rollout status deploy/cert-manager-cainjector --timeout=5m

cat <<YAML | kubectl apply -f -
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-http
spec:
  acme:
    email: $LETSENCRYPT_EMAIL
    server: https://acme-v02.api.letsencrypt.org/directory
    privateKeySecretRef:
      name: letsencrypt-http-account-key-v2
    solvers:
      - http01:
          ingress:
            ingressClassName: nginx
YAML

kubectl -n "$NAMESPACE" delete pod postgres-grants --ignore-not-found
kubectl -n "$NAMESPACE" run postgres-grants --rm -i --restart=Never --image=postgres:15 \
  --env PGPASSWORD="$POSTGRES_ADMIN_PASSWORD" \
  --env DB_USER="$DB_USER" \
  -- sh -c 'for db in identity reservations payments loyalties statistics; do psql -h postgres -U postgres -d "$db" -v ON_ERROR_STOP=1 -c "GRANT USAGE, CREATE ON SCHEMA public TO \"$DB_USER\";"; done'

if [[ "$KAFKA_BROKERS" == "kafka:9092" ]]; then
  cat <<YAML | kubectl -n "$NAMESPACE" apply -f -
apiVersion: v1
kind: Service
metadata:
  name: zookeeper
spec:
  selector:
    app.kubernetes.io/name: zookeeper
  ports:
    - name: client
      port: 2181
      targetPort: 2181
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: zookeeper
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: zookeeper
  template:
    metadata:
      labels:
        app.kubernetes.io/name: zookeeper
    spec:
      containers:
        - name: zookeeper
          image: confluentinc/cp-zookeeper:7.3.2
          ports:
            - containerPort: 2181
          env:
            - name: ZOOKEEPER_CLIENT_PORT
              value: "2181"
            - name: ZOOKEEPER_TICK_TIME
              value: "2000"
          readinessProbe:
            tcpSocket:
              port: 2181
            initialDelaySeconds: 10
            periodSeconds: 5
YAML
  kubectl -n "$NAMESPACE" rollout status deploy/zookeeper --timeout=5m || {
    kubectl -n "$NAMESPACE" describe deploy/zookeeper || true
    kubectl -n "$NAMESPACE" describe pod -l app.kubernetes.io/name=zookeeper || true
    kubectl -n "$NAMESPACE" logs deploy/zookeeper --tail=100 || true
    exit 1
  }

  cat <<YAML | kubectl -n "$NAMESPACE" apply -f -
apiVersion: v1
kind: Service
metadata:
  name: kafka
spec:
  selector:
    app.kubernetes.io/name: kafka
  ports:
    - name: kafka
      port: 9092
      targetPort: 9092
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: kafka
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: kafka
  template:
    metadata:
      labels:
        app.kubernetes.io/name: kafka
    spec:
      containers:
        - name: kafka
          image: confluentinc/cp-kafka:7.3.2
          ports:
            - containerPort: 9092
          env:
            - name: KAFKA_BROKER_ID
              value: "1"
            - name: KAFKA_ZOOKEEPER_CONNECT
              value: zookeeper:2181
            - name: KAFKA_LISTENERS
              value: PLAINTEXT://0.0.0.0:9092
            - name: KAFKA_ADVERTISED_LISTENERS
              value: PLAINTEXT://kafka:9092
            - name: KAFKA_LISTENER_SECURITY_PROTOCOL_MAP
              value: PLAINTEXT:PLAINTEXT
            - name: KAFKA_INTER_BROKER_LISTENER_NAME
              value: PLAINTEXT
            - name: KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR
              value: "1"
            - name: KAFKA_TRANSACTION_STATE_LOG_MIN_ISR
              value: "1"
            - name: KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR
              value: "1"
            - name: KAFKA_AUTO_CREATE_TOPICS_ENABLE
              value: "true"
          readinessProbe:
            tcpSocket:
              port: 9092
            initialDelaySeconds: 20
            periodSeconds: 5
YAML
  kubectl -n "$NAMESPACE" rollout status deploy/kafka --timeout=5m || {
    kubectl -n "$NAMESPACE" describe deploy/kafka || true
    kubectl -n "$NAMESPACE" describe pod -l app.kubernetes.io/name=kafka || true
    kubectl -n "$NAMESPACE" logs deploy/kafka --tail=100 || true
    exit 1
  }
fi

INGRESS_IP=$(kubectl -n ingress-nginx get svc -l app.kubernetes.io/component=controller -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')
APP_HOST=app.$INGRESS_IP.nip.io
API_HOST=api.$INGRESS_IP.nip.io
IDENTITY_HOST=identity.$INGRESS_IP.nip.io

echo "APP_HOST=$APP_HOST" >> "$GITHUB_ENV"
echo "API_HOST=$API_HOST" >> "$GITHUB_ENV"
echo "IDENTITY_HOST=$IDENTITY_HOST" >> "$GITHUB_ENV"

kubectl -n "$NAMESPACE" delete order,challenge --all --ignore-not-found || true

helm upgrade --install identity deploy/helm/app -n "$NAMESPACE" \
  --set nameOverride=identity \
  --set image.repository="$IMAGE_PREFIX/rsoi-identity" \
  --set image.tag="${GITHUB_SHA}" \
  --set-string containerPort=8084 \
  --set-string env.PORT=8084 \
  --set-string env.ISSUER="https://$IDENTITY_HOST" \
  --set-string env.DEFAULT_CLIENT_ID="$CLIENT_ID" \
  --set-string env.DEFAULT_REDIRECT_URI="https://$API_HOST/api/v1/callback" \
  --set-string env.ADMIN_USERNAME="$ADMIN_USERNAME" \
  --set-string env.ADMIN_EMAIL="$ADMIN_EMAIL" \
  --set-string env.DEFAULT_USER_USERNAME="$DEFAULT_USER_USERNAME" \
  --set-string env.DEFAULT_USER_EMAIL="$DEFAULT_USER_EMAIL" \
  --set-string secretEnv.ADMIN_PASSWORD="$ADMIN_PASSWORD" \
  --set-string secretEnv.DEFAULT_USER_PASSWORD="$DEFAULT_USER_PASSWORD" \
  --set-string secretEnv.DB_URL="postgres://$DB_USER:$DB_PASSWORD@postgres:5432/identity?sslmode=disable" \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set ingress.host="$IDENTITY_HOST" \
  --set-string "ingress.annotations.cert-manager\.io/cluster-issuer=letsencrypt-http" \
  --set "ingress.tls[0].hosts[0]=$IDENTITY_HOST" \
  --set ingress.tls[0].secretName=identity-tls

helm upgrade --install reservation deploy/helm/app -n "$NAMESPACE" \
  --set nameOverride=reservation \
  --set image.repository="$IMAGE_PREFIX/rsoi-reservation" \
  --set image.tag="${GITHUB_SHA}" \
  --set-string containerPort=8070 \
  --set-string env.PORT=8070 \
  --set-string env.IDENTITY_URL=http://identity \
  --set-string env.JWT_ISSUER="https://$IDENTITY_HOST" \
  --set-string secretEnv.DB_URL="postgres://$DB_USER:$DB_PASSWORD@postgres:5432/reservations?sslmode=disable"

helm upgrade --install payment deploy/helm/app -n "$NAMESPACE" \
  --set nameOverride=payment \
  --set image.repository="$IMAGE_PREFIX/rsoi-payment" \
  --set image.tag="${GITHUB_SHA}" \
  --set-string containerPort=8060 \
  --set-string env.PORT=8060 \
  --set-string env.IDENTITY_URL=http://identity \
  --set-string env.JWT_ISSUER="https://$IDENTITY_HOST" \
  --set-string env.KAFKA_BROKERS="$KAFKA_BROKERS" \
  --set-string env.KAFKA_PAYMENT_CANCEL_TOPIC=payment.cancel.requested \
  --set-string env.KAFKA_GROUP_ID=payment \
  --set-string secretEnv.DB_URL="postgres://$DB_USER:$DB_PASSWORD@postgres:5432/payments?sslmode=disable"

helm upgrade --install loyalty deploy/helm/app -n "$NAMESPACE" \
  --set nameOverride=loyalty \
  --set image.repository="$IMAGE_PREFIX/rsoi-loyalty" \
  --set image.tag="${GITHUB_SHA}" \
  --set-string containerPort=8050 \
  --set-string env.PORT=8050 \
  --set-string env.IDENTITY_URL=http://identity \
  --set-string env.JWT_ISSUER="https://$IDENTITY_HOST" \
  --set-string env.KAFKA_BROKERS="$KAFKA_BROKERS" \
  --set-string env.KAFKA_RESERVATION_CANCELED_TOPIC=reservation.canceled \
  --set-string env.KAFKA_RESERVATION_CREATED_TOPIC=reservation.created \
  --set-string env.KAFKA_GROUP_ID=loyalty \
  --set-string secretEnv.DB_URL="postgres://$DB_USER:$DB_PASSWORD@postgres:5432/loyalties?sslmode=disable"

helm upgrade --install statistics deploy/helm/app -n "$NAMESPACE" \
  --set nameOverride=statistics \
  --set image.repository="$IMAGE_PREFIX/rsoi-statistics" \
  --set image.tag="${GITHUB_SHA}" \
  --set-string containerPort=8040 \
  --set-string env.PORT=8040 \
  --set-string env.IDENTITY_URL=http://identity \
  --set-string env.JWT_ISSUER="https://$IDENTITY_HOST" \
  --set-string env.KAFKA_BROKERS="$KAFKA_BROKERS" \
  --set-string env.KAFKA_TOPIC="$KAFKA_TOPIC" \
  --set-string env.KAFKA_GROUP_ID=statistics \
  --set-string secretEnv.DB_URL="postgres://$DB_USER:$DB_PASSWORD@postgres:5432/statistics?sslmode=disable"

helm upgrade --install gateway deploy/helm/app -n "$NAMESPACE" \
  --set nameOverride=gateway \
  --set image.repository="$IMAGE_PREFIX/rsoi-gateway" \
  --set image.tag="${GITHUB_SHA}" \
  --set-string containerPort=8080 \
  --set-string env.PORT=8080 \
  --set-string env.RESERVATION_URL=http://reservation \
  --set-string env.PAYMENT_URL=http://payment \
  --set-string env.LOYALTY_URL=http://loyalty \
  --set-string env.IDENTITY_URL=http://identity \
  --set-string env.IDENTITY_PUBLIC_URL="https://$IDENTITY_HOST" \
  --set-string env.STATISTICS_URL=http://statistics \
  --set-string env.JWT_ISSUER="https://$IDENTITY_HOST" \
  --set-string env.CLIENT_ID="$CLIENT_ID" \
  --set-string env.REDIRECT_URI="https://$API_HOST/api/v1/callback" \
  --set-string env.UI_URL="https://$APP_HOST" \
  --set-string env.KAFKA_BROKERS="$KAFKA_BROKERS" \
  --set-string env.KAFKA_TOPIC="$KAFKA_TOPIC" \
  --set-string env.KAFKA_RESERVATION_CANCELED_TOPIC=reservation.canceled \
  --set-string env.KAFKA_RESERVATION_CREATED_TOPIC=reservation.created \
  --set-string env.KAFKA_PAYMENT_CANCEL_TOPIC=payment.cancel.requested \
  --set-string secretEnv.CLIENT_SECRET="$CLIENT_SECRET" \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set ingress.host="$API_HOST" \
  --set-string "ingress.annotations.cert-manager\.io/cluster-issuer=letsencrypt-http" \
  --set "ingress.tls[0].hosts[0]=$API_HOST" \
  --set ingress.tls[0].secretName=gateway-tls

helm upgrade --install ui deploy/helm/app -n "$NAMESPACE" \
  --set nameOverride=ui \
  --set image.repository="$IMAGE_PREFIX/rsoi-ui" \
  --set image.tag="${GITHUB_SHA}" \
  --set-string containerPort=80 \
  --set probes.enabled=false \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set ingress.host="$APP_HOST" \
  --set-string "ingress.annotations.cert-manager\.io/cluster-issuer=letsencrypt-http" \
  --set "ingress.tls[0].hosts[0]=$APP_HOST" \
  --set ingress.tls[0].secretName=ui-tls

for service in identity reservation payment loyalty statistics gateway ui; do
  kubectl -n "$NAMESPACE" rollout status "deploy/$service" --timeout=5m
done

kubectl -n "$NAMESPACE" get certificate || true
for cert in identity-tls gateway-tls ui-tls; do
  echo "Waiting for certificate/$cert"
  kubectl -n "$NAMESPACE" wait --for=condition=Ready "certificate/$cert" --timeout=10m || {
    kubectl -n "$NAMESPACE" describe "certificate/$cert" || true
    kubectl -n "$NAMESPACE" get order,challenge || true
    kubectl -n "$NAMESPACE" describe order,challenge || true
    kubectl -n cert-manager logs deploy/cert-manager --tail=100 || true
    exit 1
  }
done

echo "UI:       https://$APP_HOST"
echo "Gateway:  https://$API_HOST"
echo "Identity: https://$IDENTITY_HOST"
