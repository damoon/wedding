package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/urfave/cli/v2"
)

const weddingPort = 2376

var (
	gitHash string
	gitRef  = "latest"
)

func main() {
	app := &cli.App{
		Name:  "Wedding client",
		Usage: "Make wedding accessible.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "context", Value: filepath.Join("~", ".kube", "config"), Usage: "Config for kubectl."},
		},
		Commands: []*cli.Command{
			{
				Name:  "up",
				Usage: "Start the wedding server and port forward from localhost.",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "image", Value: fmt.Sprintf("davedamoon/wedding:%s", gitRef), Usage: "Wedding image to use."},
					&cli.BoolFlag{Name: "update", Value: false, Usage: "Replace an existing deployment of wedding."},
				},
				Action: up,
			},
			{
				Name:   "down",
				Usage:  "Remove the wedding server deployment.",
				Action: down,
			},
			{
				Name:   "version",
				Usage:  "Show the version",
				Action: version,
			},
		},
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Println(err)
		os.Exit(1)
	}
}

func version(c *cli.Context) error {
	_, err := os.Stdout.WriteString(fmt.Sprintf("version: %s\ngit commit: %s", gitRef, gitHash))
	if err != nil {
		return err
	}

	return nil
}

func down(c *cli.Context) error {
	// TODO
	return nil
}

func up(c *cli.Context) error {
	// TODO if not deployed yet or --update -> deploy wedding

	// TODO await wedding and dependencies are running

	args := c.Args()
	if args.First() == "" {
		return fmt.Errorf("command missing")
	}

	clientset, config, namespace, err := setupKubernetesClient()
	if err != nil {
		return fmt.Errorf("setup kubernetes client: %v", err)
	}

	pod, err := runningPod(c.Context, clientset, namespace)
	if err != nil {
		return fmt.Errorf("list pods: %v", err)
	}

	stopCh := make(chan struct{}, 1)
	defer close(stopCh)

	localAddr := portForward(stopCh, pod, config)

	err = executeCommand(c.Args(), localAddr)
	if err != nil {
		return fmt.Errorf("command failed with %s", err)
	}

	return nil
}

func setupKubernetesClient() (*kubernetes.Clientset, *rest.Config, string, error) {
	fmt.Println("Set up kubernetes client")

	configLoader := clientcmd.NewDefaultClientConfigLoadingRules()
	configPath := configLoader.Precedence[0]

	clientCfg, err := configLoader.Load()
	if err != nil {
		return nil, nil, "", err
	}

	config, err := clientcmd.BuildConfigFromFlags("", configPath)
	if err != nil {
		panic(err.Error())
	}

	context := clientCfg.CurrentContext
	namespace := clientCfg.Contexts[context].Namespace

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, "", err
	}

	return clientset, config, namespace, nil
}

func runningPod(ctx context.Context, clientset *kubernetes.Clientset, namespace string) (*v1.Pod, error) {
	fmt.Print("Starting service.")
	defer fmt.Println("")

	for i := 0; i < 60; i++ {
		labelSelector := metav1.LabelSelector{MatchLabels: map[string]string{"app": "wedding"}}
		listOptions := metav1.ListOptions{
			LabelSelector: labels.Set(labelSelector.MatchLabels).String(),
			Limit:         100,
		}
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, listOptions)
		if err != nil {
			return nil, fmt.Errorf("list pods: %v", err)
		}

	PODS:
		for _, pod := range pods.Items {
			if pod.Status.Phase != v1.PodRunning {
				continue
			}

			for _, conditions := range pod.Status.Conditions {
				if conditions.Status != v1.ConditionTrue {
					continue PODS
				}
			}

			return &pod, nil
		}

		time.Sleep(time.Second)
		fmt.Print(".")
	}

	return nil, fmt.Errorf("no running healthy pod not found")
}

func portForward(stopCh chan struct{}, pod *v1.Pod, cfg *rest.Config) string {
	readyCh := make(chan struct{})
	addrCh := make(chan string)

	pfr, pfw := io.Pipe()

	go func() {
		scanner := bufio.NewScanner(pfr)
		addr := ""
		for scanner.Scan() {
			ln := scanner.Text()
			if addr == "" {
				addr = extractAddress(ln)
				if addr != "" {
					addrCh <- addr
				}
			}
			fmt.Println(ln)
		}
		if err := scanner.Err(); err != nil {
			log.Printf("reading from port forward logs: %v", err)
		}
	}()

	fmt.Println("Starting port forward")
	go func() {
		defer pfw.Close()

		err := portForwardPod(
			pod.ObjectMeta.Namespace,
			pod.ObjectMeta.Name,
			weddingPort,
			cfg,
			stopCh,
			readyCh,
			pfw,
			os.Stderr,
		)

		_, ok := (<-stopCh)
		if !ok {
			return
		}

		if err != nil {
			log.Fatal(err)
		}
	}()

	localAddr := <-addrCh
	<-readyCh

	return localAddr
}

func extractAddress(ln string) string {
	re := regexp.MustCompile(`Forwarding from ((127.0.0.1|\[::1\]):[0-9]+) -> [0-9]+`)
	matches := re.FindAllStringSubmatch(ln, -1)
	if len(matches) != 1 {
		return ""
	}
	return matches[0][1]
}

func portForwardPod(
	namespace,
	podName string,
	port int,
	cfg *rest.Config,
	stopCh <-chan struct{},
	readyCh chan struct{},
	stdout io.Writer,
	errout io.Writer,
) error {
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName)
	hostIP := strings.TrimLeft(cfg.Host, "htps:/")

	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return err
	}

	dialer := spdy.NewDialer(
		upgrader,
		&http.Client{Transport: transport},
		http.MethodPost,
		&url.URL{Scheme: "https", Path: path, Host: hostIP},
	)
	fw, err := portforward.New(
		dialer,
		[]string{fmt.Sprintf("%d:%d", 0, port)},
		stopCh,
		readyCh,
		stdout,
		errout,
	)
	if err != nil {
		return err
	}
	return fw.ForwardPorts()
}

func executeCommand(args cli.Args, localAddr string) error {
	cmd := exec.Command(args.First(), args.Tail()...)
	cmd.Env = append(cmd.Env, "DOCKER_HOST=tcp://"+localAddr)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Printf("Execute command DOCKER_HOST=tcp://%s %v\n", localAddr, cmd)
	err := cmd.Run()
	if err != nil {
		return err
	}

	return nil
}
