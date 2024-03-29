# Wedding

Wedding accepts container image builds mocking the http interface of a docker daemon.\
It schedules tasks as jobs to Kubernetes.\
Images are build using buildkit.\
Images are taged using skopeo.

This enables running Tilt setups in gitlab pipelines without running a docker in docker daemon or exposing a host docker socket.\
Building images remotely allows to work from locations with slow internet upstream (home office).

## Use case 1

Using docker cli to build and push an image from within gitlab ci, without a running docker daemon.

``` bash
export DOCKER_HOST=tcp://wedding:2375
docker build -t registry/user/image:tag .
```

## Use case 2

Using tilt to set up and test an environment from within gitlab ci, without a running docker daemon.

``` bash
export DOCKER_HOST=tcp://wedding:2375
tilt ci
```

## Use case 3

Using tilt to set up a development environment without running a local docker daemon.

_Terminal 1_
``` bash
kubectl port-forward svc/wedding 2375:2375
```

_Terminal 2_
``` bash
export DOCKER_HOST=tcp://127.0.0.1:2375
tilt up
```
