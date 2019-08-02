# synchronizer
It is the implementation of *Synchronizer* https://github.com/woland7/microservices-synchronizer-config for the OpenShift platform.

Synchronizer allows to concurrently watch for OpenShift/Kubernetes Pods's PodCondition Running to become true. It does so in two distinct modes:
- *all*
  - it watches for all the Pods' PodCondition Ready of a Deployment to become true before returning
- *atleastonce*
  - it watches for at least a Pod's PodCondition Ready of a Deployment to become true before returning

At the moment it has been tested for an application bootstrapping and for a continuous deployment pipeline; in the latter case,
it can be used through the support of a Jenkins shared library, available at https://github.com/woland7/jenkins-synchronizer-sharedlibrary