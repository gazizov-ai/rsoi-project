#!/usr/bin/env bash
set -euo pipefail

newman run test-collections/end2end.json \
  -e postman-environment.json \
  --folder Baseline \
  --export-environment postman-runtime.json
mv postman-runtime.json postman-environment.json

restore_loyalty() {
  kubectl -n "$NAMESPACE" scale deploy/loyalty --replicas=1
  kubectl -n "$NAMESPACE" rollout status deploy/loyalty --timeout=5m
}
trap restore_loyalty EXIT
kubectl -n "$NAMESPACE" scale deploy/loyalty --replicas=0
kubectl -n "$NAMESPACE" wait --for=delete pod -l app.kubernetes.io/name=loyalty --timeout=120s || true
newman run test-collections/end2end.json \
  -e postman-environment.json \
  --folder "Loyalty fallback" \
  --export-environment postman-runtime.json
mv postman-runtime.json postman-environment.json
trap - EXIT
restore_loyalty

restore_payment() {
  kubectl -n "$NAMESPACE" scale deploy/payment --replicas=1
  kubectl -n "$NAMESPACE" rollout status deploy/payment --timeout=5m
}
trap restore_payment EXIT
kubectl -n "$NAMESPACE" scale deploy/payment --replicas=0
kubectl -n "$NAMESPACE" wait --for=delete pod -l app.kubernetes.io/name=payment --timeout=120s || true
newman run test-collections/end2end.json \
  -e postman-environment.json \
  --folder "Payment fallback" \
  --export-environment postman-runtime.json
mv postman-runtime.json postman-environment.json
trap - EXIT
restore_payment

restore_reservation() {
  kubectl -n "$NAMESPACE" scale deploy/reservation --replicas=1
  kubectl -n "$NAMESPACE" rollout status deploy/reservation --timeout=5m
}
trap restore_reservation EXIT
kubectl -n "$NAMESPACE" scale deploy/reservation --replicas=0
kubectl -n "$NAMESPACE" wait --for=delete pod -l app.kubernetes.io/name=reservation --timeout=120s || true
newman run test-collections/end2end.json \
  -e postman-environment.json \
  --folder "Reservation fallback" \
  --export-environment postman-runtime.json
mv postman-runtime.json postman-environment.json
trap - EXIT
restore_reservation
