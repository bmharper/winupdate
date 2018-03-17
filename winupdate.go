package main

import (
	"archive/zip"
	"bytes"
	"compress/bzip2"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const updateTimeout = 1 * time.Second

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex      = kernel32.NewProc("CreateMutexW")
	logFile              *os.File
	errNoUpdateAvailable = errors.New("No update available")
)

// Create a named mutex, and return an error if we are not the first to create it
// From https://github.com/rodolfoag/gow32
func CreateMutex(name string) (uintptr, error) {
	ret, _, err := procCreateMutex.Call(
		0,
		0,
		uintptr(unsafe.Pointer(syscall.StringToUTF16Ptr(name))),
	)
	switch int(err.(syscall.Errno)) {
	case 0:
		return ret, nil
	default:
		return ret, err
	}
}

func log(s string, p ...interface{}) {
	now := time.Now()
	msg := fmt.Sprintf("%04d-%02d-%02d %02d:%02d:%02d ", now.Year(), int(now.Month()), now.Day(), now.Hour(), now.Minute(), now.Second())
	msg += fmt.Sprintf(s, p...)
	//fmt.Println(msg)
	if logFile == nil {
		exe, err := os.Executable()
		if err == nil {
			logPath := filepath.Join(filepath.Dir(filepath.Dir(exe)), "winupdate.log")
			flags := os.O_APPEND | os.O_CREATE | os.O_WRONLY
			if inf, err := os.Stat(logPath); err == nil && inf.Size() > 5*1024*1024 {
				flags |= os.O_TRUNC
			}
			logFile, err = os.OpenFile(logPath, flags, 0644)
			if err != nil {
				return
			}
		}
	}
	logFile.WriteString(msg + "\r\n")
}

// Given an app in C:/Users/bob/AppData/Local/Company/Product[-anything], returns "Company/Product"
func appID() (string, error) {
	base, _, _, err := appDirs()
	if err != nil {
		return "", err
	}
	twoUp := filepath.Dir(filepath.Dir(base))
	return base[len(twoUp)+1:], nil
}

// base: C:/Users/bob/AppData/Local/Company/Product
// next: C:/Users/bob/AppData/Local/Company/Product-next
// temp: C:/Users/bob/AppData/Local/Company/Product-temp
func appDirs() (base, next, temp string, err error) {
	path, err := os.Executable()
	if err != nil {
		return
	}
	final := filepath.Base(filepath.Dir(path))
	if strings.Index(final, "-temp") != -1 {
		final = final[:len(final)-5]
	} else if strings.Index(final, "-next") != -1 {
		final = final[:len(final)-7]
	}
	twoUp := filepath.Dir(filepath.Dir(path))
	base = filepath.Join(twoUp, final)
	next = filepath.Join(twoUp, final+"-next")
	temp = filepath.Join(twoUp, final+"-temp")
	return
}

func isReadyForUpdate() bool {
	_, next, _, err := appDirs()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(next, "update.ready"))
	return err == nil
}

func copyFile(src, dst string) error {
	bytes, err := ioutil.ReadFile(src)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(dst, bytes, 0644)
}

func download(zipUrl string) error {
	if isReadyForUpdate() {
		// We've already prepared an update. It's up to the application now to call update.
		return nil
	}

	baseDir, nextDir, tempDir, err := appDirs()
	if err != nil {
		return err
	}

	// Download hash
	log("Download hash")
	resp, err := http.DefaultClient.Get(zipUrl + ".sha256")
	if err != nil {
		return err
	}
	hash, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}
	// Allow the sha256 file to be hex encoded, by the sha256sum tool, which emits data like "31b04e690fcf9a0dc7a460b014307332d751371b67cde77f5195b67e4643e5b5 *-"
	// Consume only the first 64 characters from the hash result
	if len(hash) >= 64 {
		hexDecoded, _ := hex.DecodeString(string(hash)[:64])
		if len(hexDecoded) == 32 {
			hash = hexDecoded
		}
	}
	if len(hash) != 32 {
		return fmt.Errorf("Server hash is %v bytes long (expected 32)", len(hash))
	}

	log("Checking whether hash is new")
	currentHashHex, err := ioutil.ReadFile(filepath.Join(baseDir, "winupdate.this.sha256"))
	if len(currentHashHex) == 64 {
		currentHash, _ := hex.DecodeString(string(currentHashHex))
		if bytes.Equal(currentHash, hash) {
			return errNoUpdateAvailable
		}
	}

	// Download archive
	log("Download archive")
	resp, err = http.DefaultClient.Get(zipUrl)
	if err != nil {
		return err
	}
	archive, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}

	// Verify hash of downloaded archive
	log("Verify hash")
	computedHash := sha256.Sum256(archive)
	if len(hash) != len(computedHash) {
		return fmt.Errorf("Hash invalid length")
	}
	if !bytes.Equal(hash, computedHash[:]) {
		return fmt.Errorf("Hash mismatch")
	}

	// Decompress bzip2
	zipArchive := archive
	if strings.LastIndex(zipUrl, ".bz2") == len(zipUrl)-4 {
		log("Decompress bzip2")
		bz2Reader := bzip2.NewReader(bytes.NewReader(archive))
		if zipArchive, err = ioutil.ReadAll(bz2Reader); err != nil {
			return err
		}
	}

	// Create -next and -temp directories
	log("Make temp dirs")
	if err := os.RemoveAll(nextDir); err != nil {
		return err
	}
	if err := os.RemoveAll(tempDir); err != nil {
		return err
	}

	// yep! this is necessary! Without this, the following mkdirs can often fail
	time.Sleep(10 * time.Millisecond)

	if err := os.MkdirAll(nextDir, 0644); err != nil {
		return err
	}
	if err := os.MkdirAll(tempDir, 0644); err != nil {
		return err
	}
	// Unpack zip archive
	log("Unzip")
	unzip, err := zip.NewReader(bytes.NewReader(zipArchive), int64(len(zipArchive)))
	if err != nil {
		return err
	}
	for _, f := range unzip.File {
		outpath := filepath.Join(nextDir, f.Name)
		file, err := os.Create(outpath)
		if err != nil {
			return err
		}
		reader, err := f.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(file, reader)
		if err != nil {
			return err
		}
		reader.Close()
		file.Close()
		os.Chtimes(outpath, f.ModTime(), f.ModTime())
	}

	// Write the hash file into nextDir, so that once this update is applied, we know not to apply it again
	if err = ioutil.WriteFile(filepath.Join(nextDir, "winupdate.this.sha256"), []byte(hex.EncodeToString(hash)), 0644); err != nil {
		return err
	}

	// Make a copy of the latest version of ourselves, in -temp. This is the version that will perform the update
	log("Copy to temp")
	err = copyFile(filepath.Join(nextDir, "winupdate.exe"), filepath.Join(tempDir, "winupdate.exe"))
	if err != nil {
		return err
	}

	// Signal that the update is ready
	log("Ready")
	file, err := os.Create(filepath.Join(nextDir, "update.ready"))
	if err != nil {
		return err
	}
	file.Close()
	return nil
}

// I don't understand why, but when I launch winupdate.exe from the host application, winupdate.exe
// ends up having an open file handle on the host application directory. Because of this open handle,
// we cannot rename the original program directory. So instead, we're forced to synchronize the two
// directories instead of using a simple rename.
func syncDirs(src, dst string) error {
	var firstErr error

	// copy from src to dst
	filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		relPath := path[len(src):]
		if !info.IsDir() {
			dstPath := filepath.Join(dst, relPath)
			log("Copy %v to %v", path, dstPath)
			copyErr := copyFile(path, dstPath)
			if copyErr != nil && firstErr == nil {
				firstErr = copyErr
			}
		}
		return nil
	})

	// delete stale items from dst
	filepath.Walk(dst, func(path string, info os.FileInfo, err error) error {
		relPath := path[len(dst):]
		if !info.IsDir() {
			srcPath := filepath.Join(src, relPath)
			if _, err := os.Stat(srcPath); err != nil {
				log("Delete %v", srcPath)
				delErr := os.Remove(path)
				if delErr != nil && firstErr != nil {
					firstErr = delErr
				}
			}
		}
		return nil
	})

	return firstErr
}

func update(mainAppExe string) error {
	base, next, _, err := appDirs()
	if err != nil {
		return err
	}

	// disable app so that it can't be launched again, while we're busy
	// This also serves the important role of testing whether we are capable of overwriting the main exe
	log("Disable app")
	mainAppPath := filepath.Join(base, mainAppExe)
	disabledAppPath := filepath.Join(base, mainAppExe) + ".disabled"
	startTime := time.Now()
	for attempt := 0; time.Now().Sub(startTime) < updateTimeout; attempt++ {
		if err = os.Rename(mainAppPath, disabledAppPath); err == nil {
			break
		}
	}
	if err != nil {
		log("Disable failed")
		return err
	}

	// I have no idea if this is valuable. I've just seen too many stories about subtle time related issues
	// in the Windows kernel, that I think it's worth giving some pause to allow all the file handles to be
	// closed, of the process that called us. It's not just the kernel either -- antiviruses are notoriously
	// bad in this space.
	time.Sleep(50 * time.Millisecond)

	// Once we start the sync, there is no going back
	log("Sync next to current")
	if err = syncDirs(next, base); err != nil {
		// failed to sync dirs, so just give up and try to relaunch
		log("Sync failed")
		//os.Rename(disabledAppPath, mainAppPath)
		cmd := exec.Command(mainAppPath)
		cmd.Start()
		return err
	}

	log("Done")
	cmd := exec.Command(mainAppPath)
	launchErr := cmd.Start()

	// Perform non-essential cleanup, and ignore any errors here
	log("Cleanup")
	os.RemoveAll(next)
	os.Remove(filepath.Join(base, "update.ready"))
	// We can't remove temp, because we are running from it

	return launchErr
}

func main() {
	// Check that we're the first instance to run, and quit if there is another one of us running, for this application
	self, err := appID()
	if err != nil {
		fmt.Printf("Failed to get appID\n")
		os.Exit(1)
	}
	self = strings.Replace(self, "\\", "-", -1)
	self = "winupdate-runner-lock-" + self
	if _, err := CreateMutex(self); err != nil {
		fmt.Printf("another instance is already running (%v)\n", err)
		os.Exit(1)
	}

	if len(os.Args) == 3 && os.Args[1] == "update" {
		log("-- update")
		err := update(os.Args[2])
		if err != nil {
			log("Error: %v", err)
		}
	} else if len(os.Args) == 3 && os.Args[1] == "download" {
		log("-- download")
		err := download(os.Args[2])
		if err != nil && err != errNoUpdateAvailable {
			log("Error: %v", err)
		}
	} else {
		fmt.Printf("winupdate [update appname.exe|download archive_url]\n")
		os.Exit(1)
	}
}
