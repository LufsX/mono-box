package storage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LufsX/mono-box/internal/box/base"
)

func scheduledFixture(t *testing.T, r *Runtime, name, source string) {
	t.Helper()
	storageWriteFixture(t, storageProfilePath(r, name), "mode: rule\n", 0600)
	storageWriteFixture(t, storageMetaPath(r, "sources/"+name+".url"), source, 0600)
}

func TestScheduledUpdateAllSourcesAndRequiresCoreValidation(t *testing.T) {
	r, _, _ := storageFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/bad" {
			_, _ = w.Write([]byte("INVALID secret"))
			return
		}
		_, _ = w.Write([]byte("mode: global\n"))
	}))
	defer server.Close()
	scheduledFixture(t, r, "a.yaml", server.URL+"/valid")
	scheduledFixture(t, r, "b.yaml", server.URL+"/bad")
	storageWriteFixture(t, storageProfilePath(r, "local.yaml"), "mode: direct\n", 0600)
	updated, err := r.ScheduledUpdate(context.Background())
	if updated != 1 || err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), server.URL) {
		t.Fatalf("%d %v", updated, err)
	}
	if storageRead(t, storageProfilePath(r, "a.yaml")) != "mode: global\n" || storageRead(t, storageProfilePath(r, "b.yaml")) != "mode: rule\n" || storageRead(t, storageProfilePath(r, "local.yaml")) != "mode: direct\n" {
		t.Fatal("invalid or local config changed")
	}
	if err := os.Remove(r.CoreBinary()); err != nil {
		t.Fatal(err)
	}
	// Even valid HTTP content must not bypass validation when no core exists.
	storageWriteFixture(t, storageProfilePath(r, "a.yaml"), "mode: rule\n", 0600)
	updated, err = r.ScheduledUpdate(context.Background())
	if err == nil || updated != 0 || storageRead(t, storageProfilePath(r, "a.yaml")) != "mode: rule\n" {
		t.Fatalf("%d %v", updated, err)
	}
	storageNoTemps(t, r)
}

func TestScheduledHTTPDoesNotHoldServiceLockAndCancels(t *testing.T) {
	r, _, _ := storageFixture(t)
	started := make(chan struct{})
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		close(started)
		<-req.Context().Done()
		close(cancelled)
	}))
	defer server.Close()
	scheduledFixture(t, r, "remote.yaml", server.URL+"/private-token")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := r.ScheduledUpdate(ctx); done <- err }()
	<-started
	lock, err := base.AcquireServiceLock(r.RunDir(), 0)
	if err != nil {
		t.Fatal("HTTP held service.lock", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("download did not cancel")
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("HTTP request leaked")
	}
	if storageRead(t, storageProfilePath(r, "remote.yaml")) != "mode: rule\n" {
		t.Fatal("cancelled download committed")
	}
}

func TestScheduledRejectsConcurrentChangeAndCancelsValidation(t *testing.T) {
	r, _, _ := storageFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		close(started)
		<-release
		_, _ = w.Write([]byte("mode: global\n"))
	}))
	defer server.Close()
	scheduledFixture(t, r, "remote.yaml", server.URL)
	done := make(chan error, 1)
	go func() { _, err := r.ScheduledUpdate(context.Background()); done <- err }()
	<-started
	storageWriteFixture(t, storageProfilePath(r, "remote.yaml"), "mode: direct\n", 0600)
	close(release)
	if err := <-done; err == nil {
		t.Fatal("overwrote concurrent edit")
	}
	if storageRead(t, storageProfilePath(r, "remote.yaml")) != "mode: direct\n" {
		t.Fatal("lost concurrent edit")
	}
	// Cancel a strict validation process, including when waiting on service.lock.
	lock, err := base.AcquireServiceLock(r.RunDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done = make(chan error, 1)
	go func() { _, err := r.ScheduledUpdate(ctx); done <- err }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduled lock wait did not cancel")
	}
	_ = lock.Close()
	storageNoTemps(t, r)
}
