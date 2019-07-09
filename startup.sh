#!/bin/bash

./synchronizer -key master.etcd-client.key -cert master.etcd-client.crt -cacert master.etcd-ca.crt waitForDependencies $1 $2 $3