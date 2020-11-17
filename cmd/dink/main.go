package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/urfave/cli/v2"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

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
					&cli.StringFlag{Name: "addr", Value: "127.0.0.1:2376", Usage: "Port for forwarding."},
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
	log.Printf("version: %v", gitRef)
	log.Printf("git commit: %v", gitHash)

	// TODO if missing or --update -> deploy wedding

	// TODO await wedding is running

	// TODO port forward until command finished or aborted

	log.Println("set up kubernetes client")

	_, _, err := setupKubernetesClient()
	if err != nil {
		return fmt.Errorf("setup kubernetes client: %v", err)
	}

	log.Println("running")

	awaitShutdown()

	log.Println("shutdown complete")

	return nil
}

func setupKubernetesClient() (*kubernetes.Clientset, string, error) {
	ns, err := ioutil.ReadFile("/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		return nil, "", fmt.Errorf("read namespace: %v", err)
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, "", err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, "", err
	}

	return clientset, string(ns), nil
}

func awaitShutdown() {
	stop := make(chan os.Signal, 2)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
}
