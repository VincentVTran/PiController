allow_k8s_contexts('default')

# Build the server image from its Dockerfile
docker_build(
    'vincentvtran/pi-controller-server',
    '.',
    dockerfile='cmd/pi-controller-server/Dockerfile',
)

# Deploy using existing k8s manifest
k8s_yaml('deployment.yaml')

# Resource config: port-forward and set dependencies
k8s_resource(
    workload='pi-controller-server',
    port_forwards=['5005:5005'],
    labels=['server'],
)
