package lab

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"time"
)

// Create is deliberately the only CLI orchestration entry point: read descriptor,
// read metadata, construct plan, publish a private file. No execution consumer.
func Create(ctx context.Context, application, out string) (Summary, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, err := newCLIReader()
	if err != nil {
		return Summary{}, err
	}
	i := inspector{r, selection{os.Getenv("DOCKER_CONTEXT"), os.Getenv("DOCKER_HOST")}, inspectSocket}
	return create(ctx, application, out, i, rand.Reader)
}

func create(ctx context.Context, application, out string, i inspector, entropy io.Reader) (Summary, error) {
	if err := ctx.Err(); err != nil {
		return Summary{}, err
	}
	d, err := readDescriptorFile(ctx, application)
	if err != nil {
		return Summary{}, err
	}
	r, base, err := openParent(out, true)
	if err != nil {
		return Summary{}, err
	}
	defer r.Close()
	if _, err = r.Lstat(base); !errors.Is(err, os.ErrNotExist) {
		return Summary{}, ErrFile
	}
	s, err := i.inspect(ctx, d)
	if err != nil {
		return Summary{}, err
	}
	p, err := buildPlan(ctx, d, s, entropy)
	if err != nil {
		return Summary{}, err
	}
	b, err := p.Encode()
	if err != nil {
		return Summary{}, err
	}
	if err = publish(ctx, r, base, b); err != nil {
		return Summary{}, err
	}
	if err = ctx.Err(); err != nil {
		return Summary{}, err
	}
	return p.summary(), nil
}

// PublicError deliberately excludes OS paths, daemon identity, CLI stderr and
// all untrusted Docker/application strings, including on partial publication.
func PublicError(err error) string {
	for _, known := range []error{context.Canceled, context.DeadlineExceeded, ErrDescriptor, ErrFile, ErrEndpoint, ErrMetadata, ErrPlan, ErrPublish, ErrInspection} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "read-only planning failed; private output may already exist"
}
