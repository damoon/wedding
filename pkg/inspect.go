package wedding

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s Service) inspect(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	image := fmt.Sprintf("wedding-registry:5000/images/%s", escapePort(vars["name"]))

	log.Println(r)

	/*
		dockerCfg, err := xRegistryAuth(r.Header.Get("X-Registry-Auth")).toDockerConfig()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(fmt.Sprintf("extract registry config: %v", err)))
			log.Printf("extract registry config: %v", err)
			return
		}

		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "wedding-docker-config-",
			},
			StringData: map[string]string{
				"config.json": dockerCfg.mustToJSON(),
			},
		}

		secretClient := s.kubernetesClient.CoreV1().Secrets(s.namespace)

		secret, err = secretClient.Create(r.Context(), secret, metav1.CreateOptions{})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			streamf(w, "Secret creation failed: %v\n", err)
			log.Printf("create secret: %v", err)
			return
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
	*/
	buildScript := fmt.Sprintf(`
set -euo pipefail
mkdir inspect-image
skopeo copy --quiet --retry-times 3 --src-tls-verify=false --dest-tls-verify=false docker://%s dir://inspect-image
skopeo inspect dir://inspect-image
`, image)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wedding-inspect-",
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
						buildScript,
					},
					// VolumeMounts: []corev1.VolumeMount{
					// 	{
					// 		MountPath: "/root/.docker",
					// 		Name:      "docker-config",
					// 	},
					// },
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
			// Volumes: []corev1.Volume{
			// 	{
			// 		Name: "docker-config",
			// 		VolumeSource: corev1.VolumeSource{
			// 			Secret: &corev1.SecretVolumeSource{
			// 				SecretName: secret.Name,
			// 			},
			// 		},
			// 	},
			// },
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	o := &bytes.Buffer{}
	err := s.executePod(r.Context(), pod, o)
	if err != nil {
		log.Printf("execute push: %v", err)
		w.WriteHeader(http.StatusNotFound)
	}

	str := o.String()
	log.Println(str)
	w.Write([]byte(str))

	// _, err = io.Copy(w, o)
	// if err != nil {
	// 	log.Printf("write inspect result: %v", err)
	// }
}
