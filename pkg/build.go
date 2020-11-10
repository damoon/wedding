package wedding

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"

	batchv1 "k8s.io/api/batch/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const (
	helpText = `
wedding only supports these arguments: context, tag, buildargs, cachefrom, cpuperiod, cpuquota, dockerfile, memory, labels, and target
%s`
)

type buildConfig struct {
	buildArgs       map[string]string
	labels          map[string]string
	cacheRepo       string
	cpuMilliseconds int
	dockerfile      string
	memoryBytes     int
	target          string
	tag             string
	noCache         bool
	registryAuth    string
	contextFilePath string
}

// ObjectStore manages access to a S3 compatible file store.
type ObjectStore struct {
	Client *s3.S3
	Bucket string
}

func (s Service) buildHandler() http.HandlerFunc {
	return func(res http.ResponseWriter, req *http.Request) {

		// res.Write([]byte(`{"aux":{"ID":"sha256:d8f38feb768dd84819b607224c07f2453412e1808b4b4e52894048073e50732d"}}`))

		//return

		ctx := req.Context()

		cfg, err := buildParameters(req)
		if err != nil {
			printBuildHelpText(res, err)
			return
		}

		err = s.objectStore.storeContext(ctx, req, cfg)
		if err != nil {
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte(fmt.Sprintf("store context: %v", err)))
			log.Printf("execute build: %v", err)
			return
		}
		defer func() {
			s.objectStore.deleteContext(ctx, cfg)
		}()

		err = s.executeBuild(ctx, cfg, res)
		if err != nil {
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte(fmt.Sprintf("execute build: %v", err)))
			log.Printf("execute build: %v", err)
			return
		}
	}
}

func buildParameters(req *http.Request) (*buildConfig, error) {
	cfg := &buildConfig{}

	asserts := map[string]string{
		// "buildargs":    "{}",
		// "cachefrom":    "[]",
		"cgroupparent": "",
		// "cpuperiod":    "0",
		// "cpuquota":     "100000",
		"cpusetcpus": "",
		"cpusetmems": "",
		"cpushares":  "0",
		// "dockerfile":   "use-case-1%2FDockerfile",
		// "labels": "{}",
		// "memory":       "1000",
		"memswap": "0",
		// "networkmode": "default", // needs two ignored values
		// "rm":      "1", // needs two ignored values
		"shmsize": "0",
		// "target":       "",
		"ulimits": "null",
		// "version": "1", // needs two ignored values
	}

	for k, v := range asserts {
		if req.URL.Query().Get(k) != v {
			return cfg, fmt.Errorf("unsupported argument %s set to '%s'", k, req.URL.Query().Get(k))
		}
	}

	networkmode := req.URL.Query().Get("networkmode")
	if networkmode != "default" && networkmode != "" { // docker uses "default", tilt uses ""
		return cfg, fmt.Errorf("unsupported argument networkmode set to '%s'", networkmode)
	}

	version := req.URL.Query().Get("version")
	if version != "1" && version != "2" { // docker uses "1", tilt uses "2"
		return cfg, fmt.Errorf("unsupported argument version set to '%s'", version)
	}

	rm := req.URL.Query().Get("rm")
	if rm != "1" && rm != "0" { // docker uses "1", tilt uses 02"
		return cfg, fmt.Errorf("unsupported argument rm set to '%s'", rm)
	}

	err := json.Unmarshal([]byte(req.URL.Query().Get("buildargs")), &cfg.buildArgs)
	if err != nil {
		return cfg, fmt.Errorf("decode buildargs: %v", err)
	}

	err = json.Unmarshal([]byte(req.URL.Query().Get("labels")), &cfg.labels)
	if err != nil {
		return cfg, fmt.Errorf("decode labels: %v", err)
	}

	// cache repo
	cachefrom := []string{}
	err = json.Unmarshal([]byte(req.URL.Query().Get("cachefrom")), &cachefrom)
	if err != nil {
		return cfg, fmt.Errorf("decode cachefrom: %v", err)
	}

	if len(cachefrom) > 1 {
		return cfg, fmt.Errorf("wedding only supports one cachefrom image")
	}
	if len(cachefrom) == 1 {
		cfg.cacheRepo = cachefrom[0]
	}

	// TODO set default cache from tag

	// cpu limit
	cpuperiod, err := strconv.Atoi(req.URL.Query().Get("cpuperiod"))
	if err != nil {
		return cfg, fmt.Errorf("parse cpu period to int: %v", err)
	}
	if cpuperiod == 0 {
		cpuperiod = 100_000 // results in 1 cpu
	}

	cpuquota, err := strconv.Atoi(req.URL.Query().Get("cpuquota"))
	if err != nil {
		return cfg, fmt.Errorf("parse cpu quota to int: %v", err)
	}
	if cpuperiod == 0 {
		cpuperiod = 100_000 // 100ms is the default of docker
	}

	cfg.cpuMilliseconds = int(1000 * float64(cpuquota) / float64(cpuperiod))

	// Dockerfile
	cfg.dockerfile = req.URL.Query().Get("dockerfile")
	if cfg.dockerfile == "" {
		cfg.dockerfile = "Dockerfile"
	}

	// memory limit
	memoryArg := req.URL.Query().Get("memory")
	if memoryArg == "" {
		memoryArg = "2147483648" // 2Gi default
	}
	memory, err := strconv.Atoi(memoryArg)
	if err != nil {
		return cfg, fmt.Errorf("parse cpu quota to int: %v", err)
	}
	cfg.memoryBytes = memory

	// target
	cfg.target = req.URL.Query().Get("target")

	// image tag
	tags := req.URL.Query()["t"]
	if len(tags) > 1 {
		return cfg, fmt.Errorf("wedding does not support setting multiple image tags at a time")
	}
	//	if len(tags) != 1 {
	//		return cfg, fmt.Errorf("image tag not set")
	//	}
	if len(tags) == 1 {
		cfg.tag = tags[0]
	}

	// disable cache
	nocache := req.URL.Query().Get("nocache")
	cfg.noCache = nocache == "1"

	// registry authentitation
	registryCfg, err := base64.StdEncoding.DecodeString(req.Header.Get("X-Registry-Config"))
	if err != nil {
		return cfg, fmt.Errorf("decode registry authentication config: %v", err)
	}
	cfg.registryAuth = string(registryCfg)

	return cfg, nil
}

func printBuildHelpText(res http.ResponseWriter, err error) {
	txt := fmt.Sprintf(helpText, err)

	res.WriteHeader(http.StatusBadRequest)

	_, err = res.Write([]byte(txt))
	if err != nil {
		log.Printf("print help text: %v", err)
	}
}

func (o ObjectStore) storeContext(ctx context.Context, req *http.Request, cfg *buildConfig) error {

	// BUG: possible OOM: loading all context into memory
	b, err := ioutil.ReadAll(req.Body)
	if err != nil {
		return fmt.Errorf("read context: %v", err)
	}

	path := fmt.Sprintf("%d.tar", time.Now().UnixNano())

	ioutil.WriteFile(path, b, os.ModePerm)

	file, err := os.Open(path)
	defer file.Close()

	put := &s3.PutObjectInput{
		Bucket:      aws.String(o.Bucket),
		Key:         aws.String(path),
		ContentType: aws.String("application/x-tar"),
		Body:        file,
	}

	_, err = o.Client.PutObjectWithContext(ctx, put)
	if err != nil {
		return fmt.Errorf("upload context to bucket: %v", err)
	}

	cfg.contextFilePath = path

	return nil

}

func (o ObjectStore) presignContext(cfg *buildConfig) (string, error) {

	req, _ := o.Client.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(o.Bucket),
		Key:    aws.String(cfg.contextFilePath),
	})

	url, err := req.Presign(time.Hour)
	if err != nil {
		return "", fmt.Errorf("presign GET %s: %v", cfg.contextFilePath, err)
	}

	return url, nil
}

func (o ObjectStore) deleteContext(ctx context.Context, cfg *buildConfig) error {
	_, err := o.Client.DeleteObjectWithContext(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(o.Bucket),
		Key:    aws.String(cfg.contextFilePath),
	})
	if err != nil {
		return err
	}

	return nil
}

func (s Service) executeBuild(ctx context.Context, cfg *buildConfig, res http.ResponseWriter) error {

	presignedContextURL, err := s.objectStore.presignContext(cfg)
	if err != nil {
		return err
	}

	buildScript := fmt.Sprintf(`
cd
pwd
wget -O - "%s" | tar -xf -
ls -la
export BUILDKITD_FLAGS="--oci-worker-no-process-sandbox"
export BUILDCTL_CONNECT_RETRIES_MAX=100
buildctl-daemonless.sh \
	  build \
	  --frontend dockerfile.v0 \
	  --local context=. \
	  --local dockerfile=. \
	  --opt filename=Dockerfile
	`, presignedContextURL)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wedding-build-",
		},
		Spec: batchv1.JobSpec{
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{
							Name:  "buildkit",
							Image: "moby/buildkit:v0.7.2-rootless",
							Command: []string{
								"sh",
								"-c",
								buildScript,
								// "date; sleep 1; date; sleep 1; date; sleep 1; date;",
							},
						},
					},
					RestartPolicy: apiv1.RestartPolicyOnFailure,
				},
			},
		},
	}

	err = s.executeJob(ctx, job, res)
	if err != nil {
		return err
	}

	return nil
}

func (s Service) executeJob(ctx context.Context, job *batchv1.Job, res http.ResponseWriter) error {
	jobsClient := s.kubernetesClient.BatchV1().Jobs(s.namespace)

	stream(res, "Creating new job.\n")

	newJob, err := jobsClient.Create(ctx, job, v1.CreateOptions{})
	if err != nil {
		streamf(res, "Job creation failed: %v\n", err)
		return fmt.Errorf("create job: %v", err)
	}

	streamf(res, "Created job %v.\n", newJob.GetName())

	defer func() {
		streamf(res, "Deleting job %v.\n", newJob.GetName())

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		propagationPolicy := v1.DeletePropagationBackground
		err = jobsClient.Delete(ctx, newJob.GetName(), v1.DeleteOptions{
			PropagationPolicy: &propagationPolicy,
		})
		if err != nil {
			streamf(res, "Job deletetion failed: %v\n", err)
			log.Printf("delete job: %v", err)
		}
	}()

watchJob:
	labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{"job-name": newJob.GetName()}}
	watchTimeout := int64(120)
	jobWatch, err := s.kubernetesClient.BatchV1().Jobs(s.namespace).
		Watch(ctx, v1.ListOptions{
			LabelSelector:  labels.Set(labelSelector.MatchLabels).String(),
			TimeoutSeconds: &watchTimeout,
		})
	if err != nil {
		return err
	}
	defer jobWatch.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context timed out")

		case e := <-jobWatch.ResultChan():
			watchedJob, ok := e.Object.(*batchv1.Job)
			if !ok {
				stream(res, "Unexpected error.\n")
				log.Panicf("unexpected type %v", e.Object)
			}

			if watchedJob.Status.Succeeded == 1 {
				stream(res, "Job finished.\n")
				return nil
			}

			if watchedJob.Status.Active == 1 {
				stream(res, "Job started.\n")
				goto showLogs
			}
		}
	}

showLogs:
	podList, err := s.kubernetesClient.CoreV1().Pods(s.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
	})

	if len(podList.Items) != 1 {
		stream(res, "Pod not found.\n")
		goto watchJob
	}

	pod := podList.Items[0]

	if pod.Status.Phase != "Running" {
		time.Sleep(time.Second)
		goto showLogs
	}

	stream(res, "Pod started.\n")

	req := s.kubernetesClient.CoreV1().Pods(s.namespace).GetLogs(pod.Name, &apiv1.PodLogOptions{Follow: true})
	podLogs, err := req.Stream(ctx)
	if err != nil {
		streamf(res, "Log streaming failed: %v\n", err)
		return fmt.Errorf("streaming logs: %v", err)
	}
	defer podLogs.Close()

	streamf(res, "Streaming logs from pod %s.\n", pod.Name)

	buf := make([]byte, 1024)
	for {
		n, err := podLogs.Read(buf)
		if n != 0 {
			stream(res, string(buf[:n]))
		}
		if err != nil {
			if err == io.EOF {
				stream(res, "End of logs reached.\n")
				return nil
			}

			return fmt.Errorf("read logs: %v", err)
		}
	}
}

func (s Service) podStatus(ctx context.Context, podName string) (apiv1.PodPhase, error) {
	pod, err := s.kubernetesClient.CoreV1().Pods(s.namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	return pod.Status.Phase, nil
}

func stream(res http.ResponseWriter, message string) error {
	b, err := json.Marshal(message)
	if err != nil {
		panic(err) // encode a string to json should not fail
	}

	_, err = res.Write([]byte(fmt.Sprintf(`{"stream": %s}`, b)))
	if err != nil {
		return err
	}

	if f, ok := res.(http.Flusher); ok {
		f.Flush()
	} else {
		return fmt.Errorf("stream can not be flushed")
	}

	return nil
}

func streamf(res http.ResponseWriter, message string, args ...interface{}) error {
	return stream(res, fmt.Sprintf(message, args...))
}
