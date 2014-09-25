package main

import (
	"fmt"
	"io/ioutil"
	"net/mail"
	"os"
	"path"
)

type Maildir struct {
	Path string

	messageCounter int
}

// Creates a new maildir, with the necessary subdirectories.
func (m *Maildir) Create() error {
	paths := []string{".", "cur", "new", "tmp"}
	for _, p := range paths {
		if err := os.Mkdir(path.Join(m.Path, p), os.ModeDir|0755); err != nil && !os.IsExist(err) {
			return err
		}
	}
	return nil
}

// Returns the next unique name for an incoming message.
func (m *Maildir) NextUniqueName() (string, error) {
	host, err := hostGetter()
	if err != nil {
		return "", err
	}
	m.messageCounter++
	return fmt.Sprintf("%d.%d_%d.%s", nowGetter().Unix(), pidGetter(), m.messageCounter, host), nil
}

// Writes a new message to the maildir.
func (m *Maildir) Write(bytes []byte) error {
	name, err := m.NextUniqueName()
	if err != nil {
		return err
	}

	tmpName := path.Join(m.Path, "tmp", name)
	curName := path.Join(m.Path, "cur", name+":2,S")

	if err = ioutil.WriteFile(tmpName, bytes, 0644); err != nil {
		return err
	}

	return os.Rename(tmpName, curName)
}

// Returns the filenames of the messages in the "cur" directory of the maildir.
func (m *Maildir) List() ([]string, error) {
	files, err := ioutil.ReadDir(path.Join(m.Path, "cur"))
	if err != nil {
		return []string{}, err
	}

	result := make([]string, 0)
	for _, file := range files {
		if !file.IsDir() {
			result = append(result, file.Name())
		}
	}
	return result, nil
}

// Returns the message in the "cur" directory of the maildir, given the
// filename (e.g. from `List()`).
func (m *Maildir) Read(name string) (*mail.Message, error) {
	file, err := os.Open(path.Join(m.Path, "cur", name))
	defer file.Close()
	if err != nil {
		return nil, err
	}

	return mail.ReadMessage(file)
}
