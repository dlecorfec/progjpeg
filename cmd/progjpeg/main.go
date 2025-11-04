// Command progjpeg is a command-line tool to encode images as progressive JPEGs.
// It can also serve the generated JPEG over HTTP for testing progressive loading using a browser
// and its throttling capabilities in dev tools.
package main

import (
	"flag"
	"fmt"
	"image"
	"net/http"
	"os"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/dlecorfec/progjpeg"
)

func main() {
	var in string
	var out string
	var hostPort string
	flag.StringVar(&in, "i", "", "Input image file path")
	flag.StringVar(&out, "o", "", "Output JPEG file path")
	flag.StringVar(&hostPort, "http", "", "Host and port for HTTP server serving output")
	flag.Parse()

	if (in == "" && hostPort == "") || out == "" {
		fmt.Fprintf(os.Stderr, "Input and output file paths must be specified")
		os.Exit(1)
	}

	// Read input image
	file, err := os.Open(in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cant open input %s: %s", in, err)
		os.Exit(1)
	}

	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cant decode input %s: %s", in, err)
		os.Exit(1)
	}

	// Create output file
	output, err := os.Create(out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cant open output %s: %s", out, err)
		os.Exit(1)
	}

	defer output.Close()

	// Encode as progressive JPEG
	err = progjpeg.Encode(output, img, &progjpeg.Options{
		Quality:     90,
		Progressive: true,
		ScanScript:  progjpeg.DefaultColorScanScript(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "cant encode output %s: %s", out, err)
		os.Exit(1)
	}

	// test server for progressive loading
	if hostPort != "" {
		fmt.Printf("Serving %s on http://%s/\n", out, hostPort)
		fileServer := func(filename string) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.ServeFile(w, r, filename)
			})
		}
		http.Handle("/", fileServer(out))
		err := http.ListenAndServe(hostPort, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "cant start http server on %s: %s", hostPort, err)
			os.Exit(1)
		}
	}
}
