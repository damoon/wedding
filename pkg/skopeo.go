package wedding

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func runSkopeoLocal(ctx context.Context, w io.Writer, processName, script, dockerJSON string) error {
	tmpHome, err := ioutil.TempDir("", "docker-secret")
	if err != nil {
		return fmt.Errorf("create tempdir for docker secret: %v", err)
	}
	defer os.RemoveAll(tmpHome)

	if dockerJSON != "" {
		err = os.Mkdir(filepath.Join(tmpHome, ".docker"), os.ModePerm)
		if err != nil {
			return fmt.Errorf("create .docker directory for docker secret: %v", err)
		}

		dockerConfigJSON := filepath.Join(tmpHome, ".docker", "config.json")
		err = ioutil.WriteFile(dockerConfigJSON, []byte(dockerJSON), os.ModePerm)
		if err != nil {
			return fmt.Errorf("write docker secret: %v", err)
		}
	}

	cmd := exec.CommandContext(
		ctx,
		"timeout",
		strconv.Itoa(int(MaxExecutionTime/time.Second)),
		"bash",
		"-c",
		script,
	)
	cmd.Stdout = w
	cmd.Stderr = w
	cmd.Env = append(cmd.Env, fmt.Sprintf("HOME=%s", tmpHome))

	return cmd.Run()
}

func (s Service) runSkopeoRemote(ctx context.Context, w io.Writer, processName, script, dockerJSON string) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("wedding-%s-", processName),
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "skopeo",
					Image: skopeoImage,
					Command: []string{
						"timeout",
						strconv.Itoa(int(MaxExecutionTime / time.Second)),
					},
					Args: []string{
						"sh",
						"-c",
						script,
					},
					Resources: corev1.ResourceRequirements{
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(skopeoCPU),
							corev1.ResourceMemory: resource.MustParse(skopeoMemory),
						},
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(skopeoCPU),
							corev1.ResourceMemory: resource.MustParse(skopeoMemory),
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	if dockerJSON != "" {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "wedding-docker-config-",
			},
			StringData: map[string]string{
				"config.json": dockerJSON,
			},
		}

		secretClient := s.kubernetesClient.CoreV1().Secrets(s.namespace)

		secret, err := secretClient.Create(ctx, secret, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("create docker.json secret: %v", err)
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			err = secretClient.Delete(ctx, secret.Name, metav1.DeleteOptions{})
			if err != nil {
				streamf(w, "Secret deletetion failed: %v\n", err)
				log.Printf("delete secret: %v", err)
			}
		}()

		pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{
			{
				MountPath: "/root/.docker",
				Name:      "docker-config",
			},
		}
		pod.Spec.Volumes = []corev1.Volume{
			{
				Name: "docker-config",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: secret.Name,
					},
				},
			},
		}
	}

	return s.executePod(ctx, pod, w)
}
