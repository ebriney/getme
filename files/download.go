package files

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/dgageot/getme/appveyor"
	"github.com/dgageot/getme/github"
	http_headers "github.com/dgageot/getme/headers"
	minio "github.com/minio/minio-go"
	"github.com/pkg/errors"
)

type Options struct {
	AuthToken   string
	S3AccessKey string
	S3SecretKey string
}

// Download downloads an url to a destination file. Additional headers can be given.
// This is helpful to pass authentication tokens.
func Download(rawURL string, destination string, options Options) error {
	url, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if url.Scheme == "s3" {
		return downloadS3(url, destination, options)
	}
	return downloadHTTP(rawURL, destination, headers(options))
}

func downloadS3(url *url.URL, destination string, options Options) error {
	s3Client, err := minio.New("s3.amazonaws.com", options.S3AccessKey, options.S3SecretKey, true)
	if err != nil {
		return err
	}

	reader, err := s3Client.GetObject(url.Host, url.Path[1:len(url.Path)])
	if err != nil {
		return err
	}
	defer reader.Close()

	destinationTmp := destination + ".tmp"
	localFile, err := os.Create(destinationTmp)
	if err != nil {
		return err
	}
	defer localFile.Close()

	stat, err := reader.Stat()
	if err != nil {
		return err
	}

	if _, err := io.CopyN(localFile, reader, stat.Size); err != nil {
		return err
	}
	return os.Rename(destinationTmp, destination)
}

func downloadHTTP(url string, destination string, headers []string) error {
	destinationTmp := destination + ".tmp"

	actualUrl := url
	actualHeaders := headers

	if github.ReleaseURL.MatchString(url) {
		log.Println("Github release url detected")

		isPublic, err := isPublicUrl(url)
		if err != nil {
			return err
		}

		if isPublic {
			log.Println("Github public release url detected")
		} else {
			log.Println("Github private release url detected")

			assetUrl, err := github.AssetUrl(url, headers)
			if err != nil {
				return err
			}

			log.Println("Github asset url is:", assetUrl)

			actualUrl = assetUrl
			actualHeaders = append(actualHeaders, "Accept=application/octet-stream")
		}

	} else if appveyor.ArtifactURL.MatchString(url) {
		log.Println("Appveyor url detected")

		artifactUrl, err := appveyor.ArtifactUrl(url, headers)
		if err != nil {
			return err
		}

		log.Println("Appveyor artifact url is:", artifactUrl)

		actualUrl = artifactUrl
	}

	err := downloadURL(actualUrl, destinationTmp, actualHeaders)
	if err != nil {
		return err
	}

	if _, err := os.Stat(destination); err == nil {
		if err := os.Remove(destination); err != nil {
			return err
		}
	}

	return os.Rename(destinationTmp, destination)
}

func isPublicUrl(url string) (bool, error) {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return false, nil
	}

	// Do not follow redirects. Only the first 404 or 302 is of interest.
	client := &http.Client{
		CheckRedirect: noCheckRedirect,
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return false, nil
	}

	return true, nil

}

func downloadURL(url string, destination string, headers []string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	if err := http_headers.Add(headers, req); err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= http.StatusBadRequest {
		return errors.New(resp.Status)
	}

	return CopyFrom(destination, 0666, resp.Body)
}

func noCheckRedirect(req *http.Request, via []*http.Request) error {
	return http.ErrUseLastResponse
}

func headers(options Options) []string {
	if options.AuthToken == "" {
		return nil
	}

	return []string{fmt.Sprintf("Authorization=Bearer %s", options.AuthToken)}
}
