// This package is the implemetantion of a primitive to suspend execution of a program by wathing on the some value
// to become true, in particular it blocks the execution of a pipeline in OpenShift/Kubernetes environment.

package main

// API for OpenShift and Kubernetes are imported
import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"os"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
	"synchronizer/utils"

	"k8s.io/kubernetes/pkg/kubectl/scheme"

	"github.com/coreos/etcd/clientv3"
	"github.com/coreos/etcd/pkg/transport"

	"github.com/openshift/api"
)

// Constants to set the semantics and constants to create the keys to be used in OpenShift
const buildConfPrefix = "/openshift.io/buildconfigs/"
const podPrefix = "/kubernetes.io/pods/"
const deployConfPrefix = "/openshift.io/deploymentconfigs/"
const deploymentPrefix = "/kubernetes.io/deployments/"
const ALL = "all"
const ATLEASTONCE = "atleastonce"

//noinspection ALL
func init() {
	api.Install(scheme.Scheme)
	api.InstallKube(scheme.Scheme)
}

// Main function. It takes all the parameter, creates the appropriate configuration connection and calls the
// appropriate function.
func main() {
	var endpoint, keyFile, certFile, caFile string
	flag.StringVar(&endpoint, "endpoint", "https://192.168.10.3:2379", "Etcd endpoint.")
	flag.StringVar(&keyFile, "key", "", "TLS client key.")
	flag.StringVar(&certFile, "cert", "", "TLS client certificate.")
	flag.StringVar(&caFile, "cacert", "", "Server TLS CA certificate.")
	flag.Parse()

	if flag.NArg() == 0 {
		fmt.Fprint(os.Stderr, "ERROR: you need to specify action: watchBuildWithTimeout <key> or watchWithTimeout <key> or get <key>\n")
		os.Exit(1)
	}
	if flag.Arg(0) == "get" && flag.NArg() == 1 {
		fmt.Fprint(os.Stderr, "ERROR: you need to specify <key> for get operation\n")
		os.Exit(1)
	}
	if flag.Arg(0) == "watchWithTimeout" && flag.NArg() == 3 {
		fmt.Fprint(os.Stderr, "ERROR: you need to specify timeout for watch operation.\n")
		os.Exit(1)
	}
	if flag.Arg(0) == "waitForDependencies" && flag.NArg() == 4 {
		fmt.Fprint(os.Stderr, "ERROR: you need to specify semantic for waitForDependecies operation.\n")
		os.Exit(1)
	}
	if flag.Arg(0) == "watchBuildWithTimeout" && flag.NArg() == 4 {
		fmt.Fprint(os.Stderr, "ERROR: you need to specify semantic for watchBuild operation.\n")
		os.Exit(1)
	}
	action := flag.Arg(0)
	namespace := ""
	key := ""
	timeout := ""
	semantic := ""
	if flag.NArg() > 1 {
		namespace = flag.Arg(1)
	}
	if flag.NArg() > 2 {
		key = flag.Arg(2)
	}
	if flag.NArg() > 3 {
		timeout = flag.Arg(3)
	}
	if flag.NArg() > 4 {
		semantic = flag.Arg(4)
		fmt.Println(semantic)
		if !(semantic == ALL || semantic == ATLEASTONCE) {
			fmt.Fprint(os.Stderr, "ERROR: Semantic can be either ALL or ATLEASTONCE.\n")
			os.Exit(1)
		}
	}

	var tlsConfig *tls.Config
	if len(certFile) != 0 || len(keyFile) != 0 || len(caFile) != 0 {
		tlsInfo := transport.TLSInfo{
			CertFile: certFile,
			KeyFile:  keyFile,
			CAFile:   caFile,
		}
		var err error
		tlsConfig, err = tlsInfo.ClientConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: unable to create client config: %v\n", err)
			os.Exit(1)
		}
	}

	config := clientv3.Config{
		Endpoints:   []string{endpoint},
		TLS:         tlsConfig,
		DialTimeout: 5 * time.Second,
	}
	client, err := clientv3.New(config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: unable to connect to etcd: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	switch action {
	case "ls":
		_, err = listKeys(client, key)
	case "get":
		err = getKey(client, namespace)
	case "watchWithTimeout":
		err = watchWithTimeout(context.Background(), namespace, key, client, timeout)
	case "watchBuildWithTimeout":
		err = watchBuildWithTimeout(namespace, key, client, timeout, semantic)
	case "waitForDependencies":
		err = waitForDependencies(namespace, key, client, timeout, semantic)
	default:
		fmt.Fprintf(os.Stderr, "ERROR: invalid action: %s\n", action)
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %s-ing %s: %v\n", action, key, err)
		os.Exit(1)
	}
	os.Exit(0)
}

// Support function. This funcion list keys. It is used to list keys from which to select the observed pods.
func listKeys(client *clientv3.Client, key string) ([]string, error) {
	var resp *clientv3.GetResponse
	var err error
	if len(key) == 0 {
		resp, err = clientv3.NewKV(client).Get(context.Background(), "/", clientv3.WithFromKey(), clientv3.WithKeysOnly())
	} else {
		resp, err = clientv3.NewKV(client).Get(context.Background(), key, clientv3.WithPrefix(), clientv3.WithKeysOnly())
	}

	if err != nil {
		return nil, err
	}

	var keys []string

	for _, kv := range resp.Kvs {
		keys = append(keys, string(kv.Key))
	}

	return keys, nil
}

// Support function. This is legacy function from etcdhelper.
func getKey(client *clientv3.Client, key string) error {
	resp, err := clientv3.NewKV(client).Get(context.Background(), key)
	if err != nil {
		return err
	}

	for _, kv := range resp.Kvs {
		obj, gvk, err := utils.Decoder.Decode(kv.Value, nil, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: unable to decode %s: %v\n", kv.Key, err)
			continue
		}
		fmt.Println(gvk)
		err = utils.Encoder.Encode(obj, os.Stdout)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: unable to encode %s: %v\n", kv.Key, err)
			continue
		}
	}

	return nil
}

//This function allow to observe for an already deployed pod to become true. The pod is specified by its name.
func watchWithTimeout(ctx context.Context, namespace string, key string, client *clientv3.Client, timeout string) error {
	c := make(chan bool)
	d, _ := time.ParseDuration(timeout)
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	pod := utils.GenerateKey(podPrefix, namespace, key)
	fmt.Printf("Watching for the following pod: %s\n", pod)
	go func() {
		watch(client, pod, ctx, c)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c:
		fmt.Println("Pod is ready. It is possible to proceed.")
	}

	return nil
}

// This function watch for a build and following deploy to be complete.
func watchBuildWithTimeout(namespace string, key string, client *clientv3.Client, timeout string, semantic string) error {
	// Timeout split with respect to the operations performed by the function

	const percentage = 0.20
	totalTime, _ := strconv.ParseFloat(timeout, 64)
	firstTimeout := totalTime * percentage
	secondTimeout := totalTime - firstTimeout
	firstD, _ := time.ParseDuration(strconv.Itoa(int(firstTimeout)) + "s")
	secondD, _ := time.ParseDuration(strconv.Itoa(int(secondTimeout)) + "s")

	//First operation. Retrieving info on observed pods.
	c := make(chan podsInfo)
	ctxFind, cancelFind := context.WithTimeout(context.Background(), firstD)
	defer cancelFind()
	var info podsInfo
	go func(semantic string) {
		fmt.Println("Finding pods...")
		findPods(client, namespace, key, ctxFind, c)
	}(semantic)
	select {
	case <-ctxFind.Done():
		return ctxFind.Err()
	case info = <-c:
		fmt.Println("Got all pods.")
	}

	//Second opearation. Waiting on the pods. At least one if ATLEASTONCE. All if all.
	var wg sync.WaitGroup
	wg.Add(len(info.pods))
	done := make(chan struct{})
	go func(wg *sync.WaitGroup) {
		wg.Wait()
		close(done)
	}(&wg)
	ctxWatch, cancelWatch := context.WithTimeout(context.Background(), secondD)
	defer cancelWatch()
	cAtLeast := make(chan bool, len(info.pods))
	for i := 0; i < len(info.pods); i++ {
		go func(i int, pods []string, modRev int64, wg *sync.WaitGroup) {
			waitForRunning(client, pods[i], ctxWatch, modRev)
			watch(client, pods[i], ctxWatch, cAtLeast)
			wg.Done()
		}(i, info.pods, info.rev, &wg)
	}
	if semantic == ALL {
		select {
		case <-done:
			fmt.Println("All replicas are ready. It is possible to proceed.")
		case <-ctxWatch.Done():
			return ctxWatch.Err()
		}
	} else if semantic == ATLEASTONCE {
		select {
		case <-cAtLeast:
			fmt.Println("At least a repclica is ready. It is possible to proceed.")
		case <-ctxWatch.Done():
			return ctxWatch.Err()
		}
	}
	return nil
}

func waitForDependencies(namespace string, key string, client *clientv3.Client, timeout string, semantic string) error{
	deployments := strings.Split(key, ",")
	numDeployments := len(deployments)
	totalTime, _ := strconv.ParseFloat(timeout, 64)
	totalTime = float64(numDeployments) * totalTime
	totalTimeout, _ := time.ParseDuration(strconv.Itoa(int(totalTime)) + "s")

	log.Printf("Watching for dependencies on %s\n", key)

	var wg sync.WaitGroup
	wg.Add(numDeployments)
	done := make(chan struct{})
	go func(wg *sync.WaitGroup) {
		wg.Wait()
		close(done)
	}(&wg)
	ctxWatchDependencies, cancelWatchDependencies := context.WithTimeout(context.Background(), totalTimeout)
	defer cancelWatchDependencies()
	for i := 0; i < numDeployments; i++ {
		go func(i int, deployments []string, wg *sync.WaitGroup) {
			log.Printf("Initializing go routine for dependency %s\\n", deployments[i])
			err := watchDeploymentWithTimeout(namespace,deployments[i],client,timeout,semantic,ctxWatchDependencies)
			if err == nil {
				wg.Done()
			}
		}(i, deployments, &wg)
	}
	select {
	case <-done:
		log.Printf("All dependencies for %s are ready. It is possible to proceed.\n", key)
	case <-ctxWatchDependencies.Done():
		return ctxWatchDependencies.Err()
	}
	return nil
}

func watchDeploymentWithTimeout(namespace string, key string, client *clientv3.Client, timeout string, semantic string, ctx context.Context) error {
	const percentage = 0.20
	totalTime, _ := strconv.ParseFloat(timeout, 64)
	firstTimeout := totalTime * percentage
	secondTimeout := totalTime - firstTimeout
	firstD, _ := time.ParseDuration(strconv.Itoa(int(firstTimeout)) + "s")
	secondD, _ := time.ParseDuration(strconv.Itoa(int(secondTimeout)) + "s")

	c := make(chan podsInfo)
	ctxFind, cancelFind := context.WithTimeout(ctx, firstD)
	defer cancelFind()
	var info podsInfo
	go func() {
		log.Printf("Initializing goroutine to find pods for dependency %s\n", key)
		findPodsPerDeployment(client, namespace, key, ctxFind, c)
	}()
	select {
	case <-ctxFind.Done():
		return ctxFind.Err()
	case info = <-c:
		log.Printf("Got all pods for %s\n", key)
	}

	//Second operation. Waiting on the pods.
	var wg sync.WaitGroup
	wg.Add(len(info.pods))
	done := make(chan struct{})
	go func(wg *sync.WaitGroup) {
		wg.Wait()
		close(done)
	}(&wg)
	cAtLeast := make(chan bool, len(info.pods))
	ctxWatch, cancelWatch := context.WithTimeout(context.Background(), secondD)
	defer cancelWatch()
	for i := 0; i < len(info.pods); i++ {
		go func(i int, pods []string, modRev int64, wg *sync.WaitGroup) {
			log.Printf("Waiting for pod %s of %s to be running\n", pods[i], key)
			waitForRunning(client, pods[i], ctxWatch, modRev)
			log.Printf("Pod %s of %s is running, now watching for it to become ready\n", pods[i], key)
			watch(client, pods[i], ctxWatch, cAtLeast)
			wg.Done()
		}(i, info.pods, info.rev, &wg)
	}
	if semantic == ALL {
		select {
		case <-done:
			log.Printf("All replica pods for %s are ready. It is possible to proceed.\n", key)
		case <-ctxWatch.Done():
			return ctxWatch.Err()
		}
	} else if semantic == ATLEASTONCE {
		select {
		case <-cAtLeast:
			log.Printf("At least a pod replica for %s is ready. It is possible to proceed.\n", key)
		case <-ctxWatch.Done():
			return ctxWatch.Err()
		}
		select {
		case <-ctxWatch.Done():
			return ctxWatch.Err()
		case <-done:
			log.Printf("All pods for %s are ready. It is possible to proceed.\n", key)
		}
	}
	return nil
}

// Support function. This function retrieves observed pods' names.
func findPods(client *clientv3.Client, namespace string, key string, ctx context.Context, c chan podsInfo) {
	buildConf := utils.GenerateKey(buildConfPrefix, namespace, key)
	modRev, _ := getModRev(client, buildConf, ctx)
	deployConf := utils.GenerateKey(deployConfPrefix, namespace, key)
	replicas, _ := getReplicasNumber(client, deployConf, ctx)
	mypodPrefix := utils.GenerateKey(podPrefix, namespace, key, "-")

	const deploysuffix = "-deploy"
	const buildsuffix = "-build"

	//r, _ := regexp.Compile("") Maybe a regex solution can be found.
	pods := make([]string, 0, replicas)
	var info podsInfo
	watcher := clientv3.NewWatcher(client)
	rch := watcher.Watch(ctx, mypodPrefix, clientv3.WithPrefix(), clientv3.WithFilterDelete(), clientv3.WithRev(modRev))
	for wresp := range rch {
		for _, ev := range wresp.Events {
			if ev.IsCreate() && !strings.HasSuffix(string(ev.Kv.Key), buildsuffix) && !strings.HasSuffix(string(ev.Kv.Key), deploysuffix) {
				pods = append(pods, string(ev.Kv.Key))
				fmt.Println(string(ev.Kv.Key))
				if len(pods) == cap(pods) {
					watcher.Close()
				}
			}
		}
	}
	info = podsInfo{rev: modRev, pods: pods}
	c <- info
}

func findPodsPerDeployment(client *clientv3.Client, namespace string, key string, ctx context.Context, c chan podsInfo) {
	deploymentConf := utils.GenerateKey(deploymentPrefix, namespace, key)
	createRev, _ := getCreateRev(client, deploymentConf, ctx)
	replicas, _ := getReplicasNumber(client, deploymentConf, ctx)
	mypodPrefix := utils.GenerateKey(podPrefix, namespace, key, "-")
	log.Printf("Finding pods for deployment %s\n", key)
	log.Printf("Replicas number for %s is %d\n", key, replicas)
	//r, _ := regexp.Compile("") Maybe a regex solution can be found.
	var info podsInfo
	pods := make([]string, 0, replicas)
	watcher := clientv3.NewWatcher(client)
	rch := watcher.Watch(ctx, mypodPrefix, clientv3.WithPrefix(), clientv3.WithFilterDelete(), clientv3.WithRev(createRev))
	for wresp := range rch {
		for _, ev := range wresp.Events {
			if ev.IsCreate() {
				pods = append(pods, string(ev.Kv.Key))
				log.Printf("Pod found is %s\n", string(ev.Kv.Key))
				if len(pods) == cap(pods) {
					watcher.Close()
				}
			}
		}
	}
	info = podsInfo{rev: createRev, pods: pods}
	c <- info

}

//Support function. This functions retrieves the modRev of a key
func getModRev(client *clientv3.Client, key string, ctx context.Context) (int64, error) {
	resp, err := clientv3.NewKV(client).Get(ctx, key)
	if err != nil {
		return 0, err
	}

	var modRev int64
	for _, kv := range resp.Kvs {
		modRev = kv.ModRevision
	}

	return modRev, nil
}

func getCreateRev(client *clientv3.Client, key string, ctx context.Context) (int64, error) {
	resp, err := clientv3.NewKV(client).Get(ctx, key)
	if err != nil {
		return 0, err
	}

	var createRev int64
	for _, kv := range resp.Kvs {
		createRev = kv.CreateRevision
	}

	return createRev, nil
}

//Support function. This functions retrieves the replicas number from the DeploymentConfig.
func getReplicasNumber(client *clientv3.Client, key string, ctx context.Context) (int, error) {
	resp, err := clientv3.NewKV(client).Get(ctx, key)
	if err != nil {
		return 0, err
	}

	var replicasNumber int
	for _, kv := range resp.Kvs {
		jq := utils.GetJsonqQuery(kv.Value)
		replicasNumber, _ = jq.Int("spec", "replicas")
	}

	return replicasNumber, nil
}

//Core function. This function wait that the specified pod becomes ready.
func watch(client *clientv3.Client, pod string, ctx context.Context, c chan bool) {
	log.Printf("First checking if pod %s is already ready\n", pod)
	if already, modRev := checkKey(client, pod); already == "True" {
		log.Printf("Pod %s is already ready.\n", pod)
		c <- true
	} else {
		log.Printf("Pod %s is not ready yet. It is necessary to watch.\n", pod)

		watcher := clientv3.NewWatcher(client)
		rch := watcher.Watch(ctx, pod, clientv3.WithRev(modRev))
		for wresp := range rch {
			for _, ev := range wresp.Events {
				jq := utils.GetJsonqQuery(ev.Kv.Value)
				rd, _ := jq.String("status", "conditions", "1", "status")
				if rd == "True" {
					watcher.Close()
				}
			}
		}
		c <- true
	}
}

//Support function. This function checks whether the pod is not ready yet.
func checkKey(client *clientv3.Client, pod string) (string, int64) {
	resp, _ := clientv3.NewKV(client).Get(context.Background(), pod)
	var rd string
	var modRev int64
	for _, kv := range resp.Kvs {
		jq := utils.GetJsonqQuery(kv.Value)
		rd, _ = jq.String("status", "conditions", "1", "status")
		modRev = kv.ModRevision

	}
	return rd, modRev
}

// Support function. This function wait for pods to enter the Running phase.
func waitForRunning(client *clientv3.Client, pod string, ctx context.Context, modRev int64) {
	watcher := clientv3.NewWatcher(client)
	rch := watcher.Watch(ctx, pod, clientv3.WithRev(modRev))
	const runningPhase = "Running"
	for wresp := range rch {
		for _, ev := range wresp.Events {
			jq := utils.GetJsonqQuery(ev.Kv.Value)
			if phase, _ := jq.String("status", "phase"); phase == runningPhase {
				watcher.Close()
			}
		}
	}
}

// Struct to contain necessary pods' information.
type podsInfo struct {
	rev int64
	pods   []string
}