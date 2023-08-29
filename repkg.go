package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/walle/targz"
)

type PackageInfo struct {
	ID          string `json:"_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	DistTags    struct {
		Latest string `json:"latest"`
	} `json:"dist-tags"`
}

func main() {
	r := gin.Default()
	r.Use(cors.New(cors.Config{
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"},
		AllowHeaders:     []string{"Authorization", "Origin", "Content-Length", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
		AllowAllOrigins:  true,
	}))

	r.StaticFS("/packages", http.Dir("./packages"))

	r.GET("/npm/:scope/:name/*version", func(c *gin.Context) {
		scope := c.Param("scope")
		name := c.Param("name")
		version := c.Param("version")
		packageName := scope + "/" + name

		if len(version) < 2 {
			version, _ = findPackageInfo(scope, name)
		}

		fetchPackage(packageName, version)
		fmt.Println("Package downloaded and extracted")

		// graceful restart or stop
		// https://gin-gonic.com/docs/examples/graceful-restart-or-stop/

		if _, err := os.Stat("packages/" + packageName); os.IsNotExist(err) {
			c.Redirect(http.StatusFound, "/packages/"+packageName+"@"+version)
		} else {
			c.String(http.StatusOK, "Hello %s", name)
		}
	})

	srv := &http.Server{
		Addr:    ":8001",
		Handler: r,
	}

	go func() {
		// service connections
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %s\n", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server with
	// a timeout of 5 seconds.
	quit := make(chan os.Signal)
	// kill (no param) default send syscanll.SIGTERM
	// kill -2 is syscall.SIGINT
	// kill -9 is syscall. SIGKILL but can"t be catch, so don't need add it
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutdown Server ...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server Shutdown:", err)
	}
	select {
	case <-ctx.Done():
		log.Println("timeout of 5 seconds.")
	}
	log.Println("Server exiting")
}

func findPackageInfo(scope string, name string) (version string, err error) {
	npmApi := "http://localhost:4873/-/verdaccio/data/sidebar/" + scope + "/" + name

	client := http.Client{
		Timeout: time.Second * 2,
	}

	req, err := http.NewRequest(http.MethodGet, npmApi, nil)
	if err != nil {
		return "", err
	}

	res, err := client.Do(req)
	if err != nil {
		return "", err
	}

	if res.Body != nil {
		defer res.Body.Close()
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	pkgInfo := PackageInfo{}
	err = json.Unmarshal(body, &pkgInfo)

	if err != nil {
		return "", err
	}

	fmt.Println(pkgInfo.DistTags.Latest)

	return pkgInfo.DistTags.Latest, nil
}

func fetchPackage(packageName string, packageVersion string) {
	registryHost := "http://localhost:4873"
	URL := registryHost + "/" + packageName + "/-/" + packageName + "-" + packageVersion + ".tgz"
	fileName := "packages/" + packageName + "/" + packageVersion + ".tgz"
	outputDir := "packages/" + packageName

	if _, err := os.Stat(outputDir + "@" + packageVersion); os.IsNotExist(err) {
		fmt.Println("Output directory does not exist, creating...")
		err := os.MkdirAll(outputDir, 0755)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		// Early return if package contents already exist
		fmt.Println("Package and version already exist, nothing to do...")
		return
	}

	err := downloadPackage(URL, fileName)
	if err != nil {
		log.Fatal(err)
	}

	err = targz.Extract(fileName, outputDir)
	if err != nil {
		log.Fatal(err)
	}

	if _, err := os.Stat(outputDir + "/package"); !os.IsNotExist(err) {
		fmt.Println("Renaming package directory to version...")
		err := os.Rename(outputDir+"/package", outputDir+"@"+packageVersion)
		if err != nil {
			log.Fatal(err)
		}
	}

	// Do not remove downloaded tgz files? I don't know, maybe

	err = os.Remove(fileName)
	if err != nil {
		log.Fatal(err)
	}
	err = os.Remove(outputDir)
	if err != nil {
		log.Fatal(err)
	}
}

func downloadPackage(URL, fileName string) error {
	response, err := http.Get(URL)
	if err != nil {
		return err
	}

	defer response.Body.Close()

	if response.StatusCode != 200 {
		return errors.New("received a non 200 response code")
	}

	file, err := os.Create(fileName)
	if err != nil {
		return err
	}

	defer file.Close()

	_, err = io.Copy(file, response.Body)
	if err != nil {
		return err
	}

	return nil
}
