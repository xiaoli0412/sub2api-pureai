package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type auditLogRetryRepo struct {
	mu         sync.Mutex
	calls      int
	inserted   int64
	errors     []error
	callSignal chan int
}

func (r *auditLogRetryRepo) BatchInsert(_ context.Context, _ []*AuditLog) (int64, error) {
	r.mu.Lock()
	r.calls++
	call := r.calls
	var err error
	if call <= len(r.errors) {
		err = r.errors[call-1]
	}
	inserted := r.inserted
	r.mu.Unlock()
	if r.callSignal != nil {
		select {
		case r.callSignal <- call:
		default:
		}
	}
	return inserted, err
}

func (r *auditLogRetryRepo) Insert(context.Context, *AuditLog) error { return nil }
func (r *auditLogRetryRepo) List(context.Context, *AuditLogFilter) (*AuditLogList, error) {
	return nil, nil
}
func (r *auditLogRetryRepo) GetByID(context.Context, int64) (*AuditLog, error) { return nil, nil }
func (r *auditLogRetryRepo) Count(context.Context) (int64, error)              { return 0, nil }
func (r *auditLogRetryRepo) TruncateAll(context.Context) error                 { return nil }
func (r *auditLogRetryRepo) DeleteBefore(context.Context, time.Time, int) (int64, error) {
	return 0, nil
}

func TestAuditLogServiceFlushBatchWithRetryEventuallySucceeds(t *testing.T) {
	repo := &auditLogRetryRepo{
		errors:     []error{errors.New("temporary 1"), errors.New("temporary 2"), nil},
		inserted:   1,
		callSignal: make(chan int, auditLogFlushRetries),
	}
	svc := NewAuditLogService(repo, nil)

	inserted, err := svc.flushBatchWithRetry([]*AuditLog{{Action: "test"}})

	require.NoError(t, err)
	require.Equal(t, int64(1), inserted)
	for expected := 1; expected <= auditLogFlushRetries; expected++ {
		select {
		case call := <-repo.callSignal:
			require.Equal(t, expected, call)
		case <-time.After(2 * time.Second):
			t.Fatalf("audit flush did not perform attempt %d", expected)
		}
	}
}

func TestAuditLogServiceWriterCountsExhaustedBatch(t *testing.T) {
	repo := &auditLogRetryRepo{
		errors:     []error{errors.New("temporary 1"), errors.New("temporary 2"), errors.New("permanent")},
		callSignal: make(chan int, auditLogFlushRetries),
	}
	svc := NewAuditLogService(repo, nil)
	svc.wg.Add(1)
	go svc.runWriter()
	svc.Record(&AuditLog{Action: "test"})

	select {
	case call := <-repo.callSignal:
		require.Equal(t, 1, call)
	case <-time.After(2 * time.Second):
		t.Fatal("audit writer did not start")
	}
	for i := 1; i < auditLogFlushRetries; i++ {
		select {
		case call := <-repo.callSignal:
			require.Equal(t, i+1, call)
		case <-time.After(2 * time.Second):
			t.Fatalf("audit writer did not perform retry %d", i+1)
		}
	}
	svc.Stop()

	health := svc.Health()
	require.Equal(t, uint64(1), health.WriteFailed)
	require.Equal(t, uint64(1), health.DroppedCount)
	require.Zero(t, health.WrittenCount)
}

func TestAuditLogServiceStopCancelsRetryBackoff(t *testing.T) {
	repo := &auditLogRetryRepo{
		errors:     []error{errors.New("temporary")},
		callSignal: make(chan int, 1),
	}
	svc := NewAuditLogService(repo, nil)
	svc.wg.Add(1)
	go svc.runWriter()
	svc.Record(&AuditLog{Action: "test"})

	select {
	case call := <-repo.callSignal:
		require.Equal(t, 1, call)
	case <-time.After(2 * time.Second):
		t.Fatal("audit writer did not start")
	}
	started := time.Now()
	svc.Stop()

	require.Less(t, time.Since(started), auditLogRetryBackoff)
	repo.mu.Lock()
	calls := repo.calls
	repo.mu.Unlock()
	require.Equal(t, 1, calls)
}

func TestAuditLogServiceRecordCountsQueueSaturation(t *testing.T) {
	svc := NewAuditLogService(&auditLogRetryRepo{}, nil)
	for i := 0; i < auditLogQueueCapacity+1; i++ {
		svc.Record(&AuditLog{Action: "test"})
	}

	health := svc.Health()
	require.Equal(t, int64(auditLogQueueCapacity), health.QueueDepth)
	require.Equal(t, uint64(1), health.DroppedCount)
}
