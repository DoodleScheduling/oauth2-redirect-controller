apiVersion: oauth2.infra.doodle.com/v1beta1
kind: OAUTH2Proxy
metadata:
  name: keycloak
  namespace: devbox-sre-1
spec:
  backend:
    serviceName: keycloak-iam-http
    servicePort: http
  host: devbox-sre-1.kubernetes.doodle-test.com
  proxyHost: oauth2-proxy.doodle-test.com
---
apiVersion: v1
kind: Service
metadata:
  name: k8soauth2-proxy-controller
  namespace: devops
spec:
  ports:
  - name: http
    port: 8080
    protocol: TCP
    targetPort: http
  selector:
    app: k8soauth2-proxy-controller
  type: ClusterIP
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    ingress.kubernetes.io/configuration-snippet: |
      proxy_set_header X-Real-IP         $remote_addr;
      proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
    ingress.kubernetes.io/force-ssl-redirect: "true"
    ingress.kubernetes.io/proxy-buffer-size: 8k
    kubernetes.io/ingress.class: external
    kubernetes.io/ingress.provider: nginx
  name: keycloak-iam-external-devbox-sre-1
  namespace: devops
spec:
  rules:
  - host: devbox-sre-1.kubernetes.doodle-test.com
    http:
      paths:
      - backend:
          service:
            name: k8soauth2-proxy-controller
            port:
              name: http
        path: /auth/realms/doodle
        pathType: ImplementationSpecific
      - backend:
          service:
            name: k8soauth2-proxy-controller
            port:
              name: http
        path: /auth/resources
        pathType: ImplementationSpecific
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    kubernetes.io/ingress.class: external
    kubernetes.io/ingress.provider: nginx
  name: oauth2-proxy
  namespace: devops
spec:
  rules:
  - host: oauth2-proxy.doodle-test.com
    http:
      paths:
      - backend:
          service:
            name: k8soauth2-proxy-controller
            port:
              name: http
        path: /
        pathType: ImplementationSpecific
