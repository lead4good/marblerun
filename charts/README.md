# MarbleRun helm charts

## Documentation

See the [Getting Started Guide](https://docs.edgeless.systems/marblerun/#/getting-started/quickstart) to set up a distributed confidential-computing app in a few simple steps.
For more comprehensive documentation, start with the [docs](https://docs.edgeless.systems/marblerun/#/).

## Add Repository (stable)

```bash
helm repo add edgeless https://helm.edgeless.systems/stable
helm repo update
```

## Install Packages (stable)

* If you are deploying on a cluster with nodes that support SGX1+FLC (e.g. AKS or minikube + Azure Standard_DC*s)

    ```bash
    helm install  marblerun edgeless/marblerun --create-namespace  --namespace marblerun
    ```

* Otherwise

    ```bash
    helm install marblerun edgeless/marblerun --create-namespace --namespace marblerun --set coordinator.resources=null --set coordinator.simulation=1 --set tolerations=null
    ```

## Configuration

The following table lists the configurable parameters of the marblerun chart and
their default values.

| Parameter                                    | Type           | Description    | Default                              |
|:---------------------------------------------|:---------------|:---------------|:-------------------------------------|
| `coordinator.dcapQpl`                        | string         | DCAP_LIBRARY needs to be "intel" if the libsgx-dcap-default-qpl is to be used, otherwise az-dcap-client is used by default | `"azure"` |
| `coordinator.clientServerHost`               | string         | Hostname of the client-api server | `"0.0.0.0"` |
| `coordinator.clientServerPort`               | int            | Port of the client-api server configuration | `4433` |
| `coordinator.hostname`                       | string         | DNS-Names for the coordinator certificate | `"localhost"` |
| `coordinator.meshServerHost`                 | string         | Hostname of the mesh-api server | `"0.0.0.0"` |
| `coordinator.meshServerPort`                 | int            | Port of the mesh-api server configuration | `2001` |
| `coordinator.replicas`                       | int            | Number of replicas for each control plane pod | `1` |
| `coordinator.sealDir`                        | string         | Path to the directory used for sealing data. Needs to be consistent with the persisten storage setup | `"/coordinator/data/"` |
| `coordinator.simulation`                     | bool           | SGX simulation settings, set to `true` if your not running on an SGX capable cluster | `false` |
| `global.coordinatorComponentLabel`           | string         | Control plane label. Do not edit | `"edgeless.systems/control-plane-component"` |
| `global.coordinatorNamespaceLabel`           | string         | Control plane label. Do not edit | `"edgeless.systems/control-plane-ns"` |
| `global.image`                               | object         | Image configuration for all components | `{"pullPolicy":"IfNotPresent","version":" v0.5.0","repository":"ghcr.io/edgelesssys"}` |
| `global.podAnnotations`                      | object         | Additional annotations to add to all pods | `{}`|
| `global.podLabels`                           | object         | Additional labels to add to all pods | `{}` |
| `marbleInjector.CABundle`                    | string         | Set this to use a custom CABundle for the MutatingWebhook | `""` |
| `marbleInjector.start`                       | bool           | Start the marbleInjector webhook | `false` |
| `marbleInjector.replicas`                    | int            | Replicas of the marbleInjector webhook | `1` |
| `nodeSelector`                               | object         | NodeSelector section, See the [K8S documentation](https://kubernetes.io/docs/concepts/configuration/assign-pod-node/#nodeselector) for more information | `{"beta.kubernetes.io/os": "linux"}` |
| `tolerations`                                | object         | Tolerations section, See the [K8S documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/) for more information | `{key:"sgx.intel.com/epc",operator:"Exists",effect:"NoSchedule"}` |
| `dcap`                                       | object         | DCAP configuration settings | `{dcap:{"pccsUrl":"https://localhost:8081/sgx/certification/v3/","useSecureCert:"TRUE"}}` |

## Add new version (maintainers)

```bash
cd <marblerun-repo>
helm package charts
mv marblerun-x.x.x.tgz <helm-repo>/stable
cd <helm-repo>
helm repo index stable --url https://helm.edgeless.systems/stable
```
