package main

import (
	"bytes"
	"testing"
)

func TestEngineeringAndSourceUploads(t *testing.T) {
	_, c := setup(t)
	c.login("admin")
	payload := []byte("\x00engineering or source content\n\xff")
	for _, name := range []string{"main.cpp", "header.hxx", "template.tpp", "types.pyi", "design.SLDPRT", "assembly.sldasm", "drawing.slddrw", "model.step", "drawing.dwg", "drawing.dxf", "part.iges", "part.x_t", "assembly.iam", "part.prt.12", "assembly.asm.2", "drawing.drw.1", "model.FCStd", "board.PcbDoc", "project.uvprojx", "main.s", "Makefile", "CMakeLists.txt", "project.zip"} {
		t.Run(name, func(t *testing.T) {
			asset := uploadForTest(t, c, name, payload, 201)
			response := downloadForTest(t, c, asset.ID, "download", 200)
			if !bytes.Equal(response.Body.Bytes(), payload) {
				t.Fatal("file content changed")
			}
			if response.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("missing nosniff")
			}
		})
	}
	for _, name := range []string{"malware.exe", "model.sldprt.exe", "bad.exe.1", "part.prt.no", "unknown"} {
		if allowedExtension(name) {
			t.Errorf("unexpected acceptance: %s", name)
		}
	}
}
