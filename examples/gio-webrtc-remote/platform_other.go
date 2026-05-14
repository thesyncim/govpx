//go:build !darwin

package main

import "image"

func newDefaultDesktopSource() (desktopSource, error) {
	return syntheticDesktopSource{size: image.Pt(desktopWidth, desktopHeight)}, nil
}

func newDefaultInputSink(desktopSource) (inputSink, error) {
	return noopInputSink{}, nil
}

type noopInputSink struct{}

func (noopInputSink) Handle(controlEvent) error { return nil }

func (noopInputSink) Close() error { return nil }
