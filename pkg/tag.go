package wedding

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/gorilla/mux"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (s Service) tagImage(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	args := r.URL.Query()

	from := fmt.Sprintf("wedding-registry:5000/digests@%s", vars["name"])
	if !strings.HasPrefix(vars["name"], "sha256:") {
		// from = fmt.Sprintf("wedding-registry:5000/images/%s", url.PathEscape(escapePort(vars["name"])))
		from = fmt.Sprintf("wedding-registry:5000/images/%s", escapePort(vars["name"]))
	}

	tag := args.Get("tag")
	if tag == "" {
		tag = "latest"
	}

	to := fmt.Sprintf(
		"wedding-registry:5000/images/%s",
		escapePort(fmt.Sprintf("%s:%s", args.Get("repo"), tag)),
	)

	// TODO add timeout for script
	buildScript := fmt.Sprintf(`
set -euxo pipefail

skopeo copy --src-tls-verify=false --dest-tls-verify=false docker://%s docker://%s
`, from, to)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "wedding-tag-",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "skopeo",
					Image: "mrliptontea/skopeo:1.2.0",
					Command: []string{
						"sh",
						"-c",
						buildScript,
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}

	b := &bytes.Buffer{}
	err := s.executePod(r.Context(), pod, b)
	if err != nil {
		log.Printf("execute tagging: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		io.Copy(w, b)
		w.Write([]byte(fmt.Sprintf("execute tagging: %v", err)))
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func escapePort(in string) string {
	re := regexp.MustCompile(`:([0-9]+/)`)
	escaped := re.ReplaceAll([]byte(in), []byte("_${1}"))
	return string(escaped)
}
