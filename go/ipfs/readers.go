package ipfs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	log "github.com/eris-ltd/eris-logger"
)

func GetFromIPFS(hash, fileName, dirName string) error {
	url := IPFSBaseGatewayUrl("") + hash
	log.WithFields(log.Fields{
		"file": fileName,
		"hash": hash,
	}).Warn("Getting file from IPFS")
	return DownloadFromUrlToFile(url, fileName, dirName, "") // no proxy for IPFS ?
}

func CatFromIPFS(fileHash string) (string, error) {
	url := IPFSBaseAPIUrl() + "cat?arg=" + fileHash
	log.WithFields(log.Fields{
		"hash": fileHash,
	}).Warn("Catting file from IPFS")
	body, err := PostAPICall(url, fileHash)

	if err != nil {
		return "", err
	}

	return string(body), nil
}

func ListFromIPFS(objectHash string) (string, error) {
	url := IPFSBaseAPIUrl() + "ls?arg=" + objectHash
	log.WithFields(log.Fields{
		"hash": objectHash,
	}).Warn("Listing file from IPFS")
	body, err := PostAPICall(url, objectHash)
	r := bytes.NewReader(body)

	type LsLink struct {
		Name, Hash string
		Size       uint64
	}
	type LsObject struct {
		Hash  string
		Links []LsLink
	}

	dec := json.NewDecoder(r)
	out := struct{ Objects []LsObject }{}
	err = dec.Decode(&out)
	if err != nil {
		return "", err
	}
	contents := out.Objects[0].Links

	res := make([]string, len(contents))
	for i, c := range contents {
		res[i] = c.Hash + " " + c.Name
	}
	result := strings.Join(res, "\n")
	return result, nil
}

func ListPinnedFromIPFS() (string, error) {
	url := IPFSBaseAPIUrl() + "pin/ls"
	log.Warn("Listing files pinned locally")
	body, err := PostAPICall(url, "")
	r := bytes.NewReader(body)

	type RefKeyObject struct {
		Type  string
		Count int
	}

	type RefKeyList struct {
		Keys map[string]RefKeyObject
	}

	var out RefKeyList
	dec := json.NewDecoder(r)
	err = dec.Decode(&out)
	if err != nil {
		return "", err
	}
	contents := out.Keys

	res := make([]string, len(contents))
	i := 0
	for c := range contents {
		res[i] = c
		i += 1
	}
	result := strings.Join(res, "\n")
	return result, nil
}

func DownloadFromUrlToFile(url0, fileName, dirName, proxyURL string) error {
	tokens := strings.Split(url0, "/")
	if fileName == "" {
		fileName = tokens[len(tokens)-1]
	}

	//use absolute paths?
	endPath := path.Join(dirName, fileName)
	if dirName != "" {
		log.WithFields(log.Fields{
			"from": url,
			"to":   endPath,
		}).Warn("Downloading")
		checkDir, err := os.Stat(dirName)
		if err != nil {
			log.Warn("Directory does not exist, creating it")
			err1 := os.MkdirAll(dirName, 0700)
			if err1 != nil {
				return fmt.Errorf("error making directory, check your permissions %v\n", err1)
			}
		}
		if !checkDir.IsDir() {
			return fmt.Errorf("path specified is not a directory, please enter a directory")
		}
	} else {
		log.WithFields(log.Fields{
			"from": url,
			"to":   fileName,
		}).Warn("Downloading")
	}

	var outputInDir *os.File
	var outputFile *os.File
	var err error
	if dirName != "" {
		outputInDir, err = os.Create(endPath)
		if err != nil {
			return err
		}
		defer outputInDir.Close()
	} else {
		outputFile, err = os.Create(fileName)
		if err != nil {
			return err
		}
		defer outputFile.Close()
	}

	transport := http.Transport{Dial: dialTimeout}

	if proxyURL == "" {
		transport = http.Transport{Proxy: nil}
	} else {
		urli := url.URL{}
		urlProxy, err := urli.Parse(proxyURL)
		if err != nil {
			return err
		}
		transport = http.Transport{Proxy: http.ProxyURL(urlProxy)}
	}

	// adding manual timeouts as IPFS hangs for a while
	client := http.Client{
		Transport: &transport,
	}

	response, err := client.Get(url0)
	if err != nil {
		return err
	}
	defer response.Body.Close()

	var checkBody []byte
	if dirName != "" {
		_, err = io.Copy(outputInDir, response.Body)
		if err != nil {
			return err
		}
		checkBody, err = ioutil.ReadFile(endPath)
		if err != nil {
			return err
		}
	} else {
		_, err = io.Copy(outputFile, response.Body)
		if err != nil {
			return err
		}
		checkBody, err = ioutil.ReadFile(fileName)
		if err != nil {
			return err
		}
	}

	//deal with ipfs' error ungracefully. maybe we want to maintain our own fork?
	//or could run `cache` under the hood, so user doesn't even see error (although we probably shouldn't pin by default)
	if string(checkBody) == "Path Resolve error: context deadline exceeded" {
		//this won't work unless we `eris files cache --csv (which will be especially needed to deal with this error)
		return fmt.Errorf("A timeout occured while trying to reach IPFS. Run `eris files cache [hash], wait 5-10 seconds, then run `eris files [cmd] [hash]`")
	}

	return nil
}

// --------------------------------------------------------------
// Helper functions

var timeout = time.Duration(10 * time.Second)

func dialTimeout(network, addr string) (net.Conn, error) {
	return net.DialTimeout(network, addr, timeout)
}
