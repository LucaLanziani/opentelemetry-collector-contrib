// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package reader

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/fileconsumer/internal/filetest"
	"github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/fileconsumer/internal/fingerprint"
	internaltime "github.com/open-telemetry/opentelemetry-collector-contrib/pkg/stanza/internal/time"
)

func TestFileReader_FingerprintUpdated(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	temp := filetest.OpenTemp(t, tempDir)
	tempCopy := filetest.OpenFile(t, temp.Name())

	f, sink := testFactory(t)
	fp, err := f.NewFingerprint(temp)
	require.NoError(t, err)

	reader, err := f.NewReader(tempCopy, fp)
	require.NoError(t, err)
	defer reader.Close()

	filetest.WriteString(t, temp, "testlog1\n")
	reader.ReadToEnd(context.Background())
	sink.ExpectToken(t, []byte("testlog1"))
	require.Equal(t, fingerprint.New([]byte("testlog1\n")), reader.Fingerprint)
}

// Test that a fingerprint:
// - Starts empty
// - Updates as a file is read
// - Stops updating when the max fingerprint size is reached
// - Stops exactly at max fingerprint size, regardless of content
func TestFingerprintGrowsAndStops(t *testing.T) {
	t.Parallel()

	// Use a number with many factors.
	// Sometimes fingerprint length will align with
	// the end of a line, sometimes not. Test both.
	fpSize := 360

	// Use prime numbers to ensure variation in
	// whether or not they are factors of fpSize
	lineLens := []int{3, 5, 7, 11, 13, 17, 19, 23, 27}

	for _, lineLen := range lineLens {
		t.Run(fmt.Sprintf("%d", lineLen), func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			temp := filetest.OpenTemp(t, tempDir)
			tempCopy := filetest.OpenFile(t, temp.Name())

			f, _ := testFactory(t, withSinkChanSize(3*fpSize/lineLen), withFingerprintSize(fpSize))
			fp, err := f.NewFingerprint(temp)
			require.NoError(t, err)
			require.Equal(t, fingerprint.New([]byte("")), fp)

			reader, err := f.NewReader(tempCopy, fp)
			require.NoError(t, err)
			defer reader.Close()

			// keep track of what has been written to the file
			var fileContent []byte

			// keep track of expected fingerprint size
			expectedFP := 0

			// Write lines until file is much larger than the length of the fingerprint
			for len(fileContent) < 2*fpSize {
				expectedFP += lineLen
				if expectedFP > fpSize {
					expectedFP = fpSize
				}

				line := string(filetest.TokenWithLength(lineLen-1)) + "\n"
				fileContent = append(fileContent, []byte(line)...)

				filetest.WriteString(t, temp, line)
				reader.ReadToEnd(context.Background())
				require.Equal(t, fingerprint.New(fileContent[:expectedFP]), reader.Fingerprint)
			}
		})
	}
}

// This is same test like TestFingerprintGrowsAndStops, but with additional check for fingerprint size check
// Test that a fingerprint:
// - Starts empty
// - Updates as a file is read
// - Stops updating when the max fingerprint size is reached
// - Stops exactly at max fingerprint size, regardless of content
// - Do not change size after fingerprint configuration change
func TestFingerprintChangeSize(t *testing.T) {
	t.Parallel()

	// Use a number with many factors.
	// Sometimes fingerprint length will align with
	// the end of a line, sometimes not. Test both.
	fpSize := 360

	// Use prime numbers to ensure variation in
	// whether or not they are factors of fpSize
	lineLens := []int{3, 4, 5, 6, 7, 8, 11, 12, 13, 17, 19, 23, 27, 36}

	for _, lineLen := range lineLens {
		t.Run(fmt.Sprintf("%d", lineLen), func(t *testing.T) {
			t.Parallel()

			f, _ := testFactory(t, withSinkChanSize(3*fpSize/lineLen), withFingerprintSize(fpSize))

			tempDir := t.TempDir()
			temp := filetest.OpenTemp(t, tempDir)

			fp, err := f.NewFingerprint(temp)
			require.NoError(t, err)
			require.Equal(t, fingerprint.New([]byte("")), fp)

			reader, err := f.NewReader(filetest.OpenFile(t, temp.Name()), fp)
			require.NoError(t, err)

			// keep track of what has been written to the file
			var fileContent []byte

			// keep track of expected fingerprint size
			expectedFP := 0

			// Write lines until file is much larger than the length of the fingerprint
			for len(fileContent) < 2*fpSize {
				expectedFP += lineLen
				if expectedFP > fpSize {
					expectedFP = fpSize
				}

				line := string(filetest.TokenWithLength(lineLen-1)) + "\n"
				fileContent = append(fileContent, []byte(line)...)

				filetest.WriteString(t, temp, line)
				reader.ReadToEnd(context.Background())
				require.Equal(t, fingerprint.New(fileContent[:expectedFP]), reader.Fingerprint)
			}

			// Recreate the factory with a larger fingerprint size
			fpSizeUp := fpSize * 2
			f, _ = testFactory(t, withSinkChanSize(3*fpSize/lineLen), withFingerprintSize(fpSizeUp))

			// Recreate the reader with the new factory
			reader, err = f.NewReaderFromMetadata(filetest.OpenFile(t, temp.Name()), reader.Close())
			require.NoError(t, err)

			line := string(filetest.TokenWithLength(lineLen-1)) + "\n"
			fileContent = append(fileContent, []byte(line)...)

			filetest.WriteString(t, temp, line)
			reader.ReadToEnd(context.Background())
			require.Equal(t, fingerprint.New(fileContent[:fpSizeUp]), reader.Fingerprint)

			// Recreate the factory with a smaller fingerprint size
			fpSizeDown := fpSize / 2
			f, _ = testFactory(t, withSinkChanSize(3*fpSize/lineLen), withFingerprintSize(fpSizeDown))

			// Recreate the reader with the new factory
			reader, err = f.NewReaderFromMetadata(filetest.OpenFile(t, temp.Name()), reader.Close())
			require.NoError(t, err)

			line = string(filetest.TokenWithLength(lineLen-1)) + "\n"
			fileContent = append(fileContent, []byte(line)...)

			filetest.WriteString(t, temp, line)
			reader.ReadToEnd(context.Background())
			require.Equal(t, fingerprint.New(fileContent[:fpSizeDown]), reader.Fingerprint)
		})
	}
}

func TestFlushPeriodEOF(t *testing.T) {
	tempDir := t.TempDir()
	temp := filetest.OpenTemp(t, tempDir)
	// Create a long enough initial token, so the scanner can't read the whole file at once
	aContentLength := 2 * 16 * 1024
	content := []byte(strings.Repeat("a", aContentLength))
	content = append(content, '\n', 'b')
	_, err := temp.WriteString(string(content))
	require.NoError(t, err)

	flushPeriod := time.Millisecond
	f, sink := testFactory(t, withFlushPeriod(flushPeriod))
	fp, err := f.NewFingerprint(temp)
	require.NoError(t, err)
	r, err := f.NewReader(temp, fp)
	require.NoError(t, err)
	assert.Equal(t, int64(0), r.Offset)

	clock := internaltime.NewAlwaysIncreasingClock()
	internaltime.Now = clock.Now
	internaltime.Since = clock.Since
	defer func() {
		internaltime.Now = time.Now
		internaltime.Since = time.Since
	}()

	// First ReadToEnd should not emit only the terminated token
	r.ReadToEnd(context.Background())
	sink.ExpectToken(t, content[0:aContentLength])

	// Advance time past the flush period
	clock.Advance(2 * flushPeriod)

	// Second ReadToEnd should emit the unterminated token because of flush timeout
	r.ReadToEnd(context.Background())
	sink.ExpectToken(t, []byte{'b'})
}
