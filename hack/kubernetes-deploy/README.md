# etcd on Kubernetes

This is an example setting up etcd as a set of pods and services running on top of kubernetes. Using:

The example intentionally pins the upstream `v2.3.7` Linux/AMD64 image by its registry digest. That engine version matches the compatibility source in this repository. The preserved image uses the Docker schema 1 manifest format, so runtimes that reject schema 1 cannot use this example. Update the tag and digest together only after compatibility review; never replace the reference with a floating tag.

```
$ kubectl create -f etcd.yml 
services/etcd-client
pods/etcd0
services/etcd0
pods/etcd1
services/etcd1
pods/etcd2
services/etcd2
$ # now deploy a service that consumes etcd, such as vulcand
$ kubectl create -f vulcand.yml
```

TODO:

- create a replication controller like service that knows how to add and remove nodes from the cluster correctly
- use kubernetes secrets API to configure TLS for etcd clients and peers
