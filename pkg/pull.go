package wedding

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

func (s Service) pullImage(w http.ResponseWriter, r *http.Request) {
	args := r.URL.Query()

	fromImage := args.Get("fromImage")
	if fromImage == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("image to pull is missing"))
		return
	}

	pullTag := args.Get("tag")
	if pullTag == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("tag to pull is missing"))
		return
	}

	if args.Get("repo") != "" {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte("repo is not supported"))
		return
	}

	if args.Get("fromSrc") != "" {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte("import from a file is not supported"))
		return
	}

	if args.Get("message") != "" {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte("message is not supported"))
		return
	}

	if args.Get("platform") != "" {
		w.WriteHeader(http.StatusNotImplemented)
		w.Write([]byte("platform is not supported"))
		return
	}

	from := fmt.Sprintf("%s:%s", fromImage, pullTag)
	to := fmt.Sprintf("wedding-registry:5000/images/%s", escapePort(from))

	dockerCfg, err := xRegistryAuth(r.Header.Get("X-Registry-Auth")).toDockerConfig()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("extract registry config: %v", err)))
		log.Printf("extract registry config: %v", err)
		return
	}

	script := fmt.Sprintf(`skopeo copy --retry-times 3 --dest-tls-verify=false docker://%s docker://%s`, from, to)

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	scheduler := s.runSkopeoRemote
	err = semSkopeo.Acquire(ctx, 1)
	if err == nil {
		log.Printf("pull locally %s", from)
		defer semSkopeo.Release(1)
		scheduler = runSkopeoLocal
	} else {
		log.Printf("pull scheduled %s", from)
	}

	o := &output{w: w}
	err = scheduler(r.Context(), o, "pull", script+" || "+script, dockerCfg.mustToJSON())
	if err != nil {
		log.Printf("execute pull: %v", err)
		o.Errorf("execute pull: %v", err)
	}
}
