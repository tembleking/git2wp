package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/pkg/errors"
	"github.com/robbiet480/go-wordpress"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing/object"
)

func main() {
	var (
		gitLocalRepository = "repo"
		gitRepository      = os.Getenv("GIT_REPO")
		wordpressUrl       = os.Getenv("WP_URL")
		wordpressUsername  = os.Getenv("WP_USER")
		wordpressPassword  = os.Getenv("WP_PASSWD")
	)

	fmt.Println("Retrieving the repository...")
	repository, err := getRepository(gitRepository, gitLocalRepository)
	if err != nil {
		panic(err)
	}

	fmt.Println("Retrieving the last commit...")
	commit, err := getLastCommit(repository)
	if err != nil {
		panic(err)
	}

	imagesFound, _ := findAllImagesForCommit(commit)
	if err != nil {
		panic(err)
	}
	fmt.Println(len(imagesFound), "image(s) found in the repository.")

	client, err := createWordpressClient(wordpressUsername, wordpressPassword, wordpressUrl)
	if err != nil {
		panic(err)
	}

	fmt.Println("Retrieving Wordpress images...")
	remoteImages, err := findAllRemoteImages(client)
	if err != nil {
		panic(err)
	}
	fmt.Println(len(remoteImages), "remote image(s) found.")

	missingImages := getMissingRemoteImages(imagesFound, remoteImages)
	fmt.Println(len(missingImages), "missing image(s) will be uploaded.")

	uploadMissingImages(client, missingImages)
}

func getRepository(url, path string) (repository *git.Repository, err error) {
	repository, err = git.PlainClone(path, true, &git.CloneOptions{
		URL:          url,
		SingleBranch: true,
	})
	if err == git.ErrRepositoryAlreadyExists {
		repository, err = git.PlainOpen(path)

		// Try to update the repository
		if err = repository.Fetch(&git.FetchOptions{}); err == git.NoErrAlreadyUpToDate {
			err = nil
		}
	}
	return
}

func getLastCommit(repository *git.Repository) (commit *object.Commit, err error) {
	iter, err := repository.CommitObjects()
	if err != nil {
		return
	}

	commits := []*object.Commit{}
	iter.ForEach(func(commit *object.Commit) error {
		commits = append(commits, commit)
		return nil
	})

	sort.Slice(commits, func(i, j int) bool {
		return commits[j].Committer.When.Before(commits[i].Committer.When)
	})
	if len(commits) == 0 {
		err = errors.New("tree is empty, there's no last commit to retrieve")
		return
	}

	commit = commits[0]
	return
}

func getMissingRemoteImages(s1 []*object.File, s2 []string) (result []*object.File) {
outer:
	for _, fileToSearch := range s1 {
		for _, found := range s2 {
			if strings.ToLower(path.Base(fileToSearch.Name)) == strings.ToLower(found) {
				continue outer
			}
		}
		// not found
		result = append(result, fileToSearch)
	}
	return
}

func uploadMissingImages(client *wordpress.Client, missingImages []*object.File) {
	wg := sync.WaitGroup{}
	for _, image := range missingImages {
		readCloser, err := image.Reader()
		if err != nil {
			log.Println(err)
			continue
		}
		defer readCloser.Close()

		data, err := ioutil.ReadAll(readCloser)

		wg.Add(1)
		go func(data []byte, imageName string) {
			defer wg.Done()
			contentType := http.DetectContentType(data)
			fmt.Printf("Uploading %s (%s)\n", imageName, contentType)
			imageCreated, response, err := client.Media.Create(context.Background(), &wordpress.MediaUploadOptions{
				Data:        data,
				Filename:    imageName,
				ContentType: contentType,
			})
			if err != nil {
				if response.StatusCode == 502 {
					log.Println("error uploading", imageName, response.Status)
				} else {
					log.Println("error uploading", imageName, err)
				}
				return
			}
			response.Body.Close()

			fmt.Printf("Uploaded image %s to url %s.\n", imageCreated.Title.Rendered, imageCreated.SourceURL)
		}(data, path.Base(image.Name))
	}
	wg.Wait()
}

func findAllRemoteImages(client *wordpress.Client) (remoteImages []string, err error) {
	options := &wordpress.MediaListOptions{}
	options.PerPage = 100
	options.MediaType = "image"
	options.Page = 1


	for {
		media, response, err := client.Media.List(context.Background(), options)
		if err != nil || len(media) == 0 {
			break
		}
		response.Body.Close()

		for _, image := range media {
			parse, _ := url.Parse(image.SourceURL)
			remoteImages = append(remoteImages, path.Base(parse.Path))
		}
		fmt.Print(len(remoteImages), " remote image(s) found.\r")

		options.Page++
	}
	return
}

func createWordpressClient(wordpressUsername string, wordpressPassword string, wordpressUrl string) (client *wordpress.Client, err error) {
	auth := wordpress.BasicAuthTransport{
		Username: wordpressUsername,
		Password: wordpressPassword,
	}
	return wordpress.NewClient(fmt.Sprintf("%s/wp-json/", wordpressUrl), auth.Client())
}

func findAllImagesForCommit(commit *object.Commit) (imagesFound []*object.File, err error) {
	iter, err := commit.Files()
	if err != nil {
		return
	}

	iter.ForEach(func(file *object.File) error {
		if strings.Contains(file.Name, "_images") {
			file.Name = strings.Replace(file.Name, " ", "_", -1)
			imagesFound = append(imagesFound, file)
		}
		return nil
	})

	return
}
