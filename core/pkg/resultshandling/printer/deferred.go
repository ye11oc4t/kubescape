package printer

import (
	"context"
	"errors"

	"github.com/kubescape/kubescape/v4/core/cautils"
)

// DeferredPrinter keeps an existing output file intact until there is a
// renderable result. It delegates format/path behavior to the original printer.
type DeferredPrinter struct {
	IPrinter
	path           string
	validate       func() error
	opened, closed bool
	closeErr       error
}

func NewDeferredPrinter(p IPrinter, validate func() error) *DeferredPrinter {
	return &DeferredPrinter{IPrinter: p, validate: validate}
}
func (p *DeferredPrinter) SetWriter(_ context.Context, path string) error {
	p.path = path
	return nil
}
func (p *DeferredPrinter) SupportsPartialScan() bool {
	capability, ok := p.IPrinter.(interface{ SupportsPartialScan() bool })
	return ok && capability.SupportsPartialScan()
}
func (p *DeferredPrinter) ActionPrint(ctx context.Context, data *cautils.OPASessionObj, images []cautils.ImageScanData) error {
	if p.closed {
		return errors.New("output writer already closed")
	}
	if !p.opened {
		if p.validate != nil {
			if err := p.validate(); err != nil {
				return err
			}
		}
		p.opened = true
		if err := p.IPrinter.SetWriter(ctx, p.path); err != nil {
			return err
		}
	}
	return p.IPrinter.ActionPrint(ctx, data, images)
}
func (p *DeferredPrinter) CloseWriter() error {
	if p.closed {
		return p.closeErr
	}
	p.closed = true
	if !p.opened {
		return nil
	}
	if c, ok := p.IPrinter.(interface{ CloseWriter() error }); ok {
		p.closeErr = c.CloseWriter()
	} else if c, ok := p.IPrinter.(interface{ CloseWriter() }); ok {
		c.CloseWriter()
	}
	return p.closeErr
}
