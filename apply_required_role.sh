kubectl apply -f - <<'EOF'
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: podtracker-manager
rules:
  - apiGroups: ["crd.devops.toolbox"]
    resources: ["podtrackers"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: podtracker-manager-binding
subjects:
  - kind: ServiceAccount
    name: default
    namespace: default
roleRef:
  kind: ClusterRole
  name: podtracker-manager
  apiGroup: rbac.authorization.k8s.io
EOF
