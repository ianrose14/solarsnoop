package util

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOffsetIntoDay(t *testing.T) {
	eastern, err := time.LoadLocation("US/Eastern")
	require.NoError(t, err, "failed to load eastern timezone")

	testCases := []struct {
		in  time.Time
		out time.Duration
	}{
		{time.Date(2000, time.July, 1, 0, 0, 0, 0, time.UTC), 0},
		{time.Date(1984, time.March, 21, 0, 0, 0, 0, time.UTC), 0},
		{time.Date(2000, time.July, 1, 0, 0, 0, 0, eastern), 0},
		{time.Date(2000, time.July, 1, 0, 0, 0, 9876, time.UTC), 9876},
		{time.Date(2000, time.July, 1, 0, 0, 3, 0, time.UTC), 3 * time.Second},
		{time.Date(2000, time.July, 1, 0, 48, 0, 0, time.UTC), 48 * time.Minute},
		{time.Date(2000, time.July, 1, 1, 0, 0, 0, time.UTC), time.Hour},
		{time.Date(2000, time.July, 18, 16, 10, 15, 0, time.UTC), 58215 * time.Second},
	}

	for i, testCase := range testCases {
		t.Run(fmt.Sprintf("test case %d", i), func(t *testing.T) {
			out := OffsetIntoDay(testCase.in)
			require.Equal(t, testCase.out, out, "wrong result")
		})
	}
}
