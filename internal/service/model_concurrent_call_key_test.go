package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"rockbolt-pullout-zonal-closure/internal/domain"
)

type modelBlockingInstrument struct {
	entered chan domain.InstrumentRequest
	release chan struct{}

	mu    sync.Mutex
	calls []domain.InstrumentRequest
}

func (p *modelBlockingInstrument) Call(ctx context.Context, req domain.InstrumentRequest) (domain.InstrumentResult, error) {
	p.mu.Lock()
	p.calls = append(p.calls, req)
	p.mu.Unlock()
	p.entered <- req
	select {
	case <-p.release:
		reading := int64(1)
		return domain.InstrumentResult{Result: domain.CallAccepted, Reading: &reading}, nil
	case <-ctx.Done():
		return domain.InstrumentResult{}, ctx.Err()
	}
}

func (p *modelBlockingInstrument) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

func TestModel_ConcurrentReadingArbitratesBeforeInstrument(t *testing.T) {
	tests := []struct {
		name                     string
		samples                  [2]string
		wantEnteredBeforeRelease int
		wantAccepted             int
		wantConflicts            int
		checkCommittedDuplicate  bool
	}{
		{
			name:                     "same call key is arbitrated before the device",
			samples:                  [2]string{"sample-1", "sample-1"},
			wantEnteredBeforeRelease: 1,
			wantAccepted:             1,
			wantConflicts:            1,
			checkCommittedDuplicate:  true,
		},
		{
			name:                     "different samples do not share a device-command lock",
			samples:                  [2]string{"sample-1", "sample-2"},
			wantEnteredBeforeRelease: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestService(t)
			taskID := lockSampleVerifyLease(t, svc)
			before, err := svc.GetTask(context.Background(), taskID)
			if err != nil {
				t.Fatalf("get task before submissions: %v", err)
			}
			port := &modelBlockingInstrument{
				entered: make(chan domain.InstrumentRequest, 2),
				release: make(chan struct{}),
			}
			svc.SetInstrument(port)

			type outcome struct {
				result *ReadingResult
				err    error
			}
			outcomes := make(chan outcome, 2)
			submit := func(index int) {
				result, submitErr := svc.SubmitReading(context.Background(), "model-concurrent-op-"+string(rune('a'+index)), ReadingInput{
					TaskID: taskID, SampleID: tt.samples[index], Stage: domain.StagePreload,
					Load: 1000, Displacement: 100,
					DevicePuller: "puller-1", DevicePump: "pump-1", DeviceDisplacement: "disp-1",
				})
				outcomes <- outcome{result: result, err: submitErr}
			}

			go submit(0)
			select {
			case <-port.entered:
			case <-time.After(2 * time.Second):
				t.Fatal("first submission did not reach InstrumentPort.Call")
			}
			go submit(1)

			if tt.wantEnteredBeforeRelease == 2 {
				select {
				case second := <-port.entered:
					if second.CallKey == "" {
						t.Fatal("second instrument request has an empty call key")
					}
				case <-time.After(2 * time.Second):
					t.Fatal("different-sample submission was serialized behind an unrelated call key")
				}
			} else {
				select {
				case duplicate := <-port.entered:
					t.Fatalf("duplicate reached InstrumentPort.Call before arbitration: call_key=%q", duplicate.CallKey)
				case <-time.After(500 * time.Millisecond):
				}
			}
			if got := port.callCount(); got != tt.wantEnteredBeforeRelease {
				t.Fatalf("instrument calls before release = %d, want %d", got, tt.wantEnteredBeforeRelease)
			}

			close(port.release)
			accepted, conflicts := 0, 0
			for i := 0; i < 2; i++ {
				select {
				case got := <-outcomes:
					if got.err == nil && got.result != nil && got.result.Accepted {
						accepted++
					} else if IsConflict(got.err) {
						conflicts++
					}
				case <-time.After(2 * time.Second):
					t.Fatal("submission did not finish after instrument release")
				}
			}

			if !tt.checkCommittedDuplicate {
				return
			}
			if accepted != tt.wantAccepted || conflicts != tt.wantConflicts {
				t.Fatalf("outcomes: accepted=%d conflicts=%d, want accepted=%d conflicts=%d", accepted, conflicts, tt.wantAccepted, tt.wantConflicts)
			}
			evidence, err := svc.GetEvidence(context.Background(), taskID)
			if err != nil {
				t.Fatalf("get evidence: %v", err)
			}
			afterDuplicate, err := svc.GetTask(context.Background(), taskID)
			if err != nil {
				t.Fatalf("get task after duplicate: %v", err)
			}
			if len(evidence) != 1 || afterDuplicate.Version != before.Version+1 {
				t.Fatalf("duplicate commit wrote evidence/version more than once: evidence=%d version delta=%d", len(evidence), afterDuplicate.Version-before.Version)
			}

			followUp, err := svc.SubmitReading(context.Background(), "model-follow-up", ReadingInput{
				TaskID: taskID, SampleID: "sample-1", Stage: domain.StageLoad, LoadLevel: 1,
				Load: 50000, Displacement: 900,
				DevicePuller: "puller-1", DevicePump: "pump-1", DeviceDisplacement: "disp-1",
			})
			if err != nil || followUp == nil || !followUp.Accepted {
				t.Fatalf("legal next stage was not accepted: result=%+v err=%v", followUp, err)
			}
			evidence, err = svc.GetEvidence(context.Background(), taskID)
			if err != nil {
				t.Fatalf("get evidence after follow-up: %v", err)
			}
			if len(evidence) != 2 || followUp.Task.Version != before.Version+2 || port.callCount() != 2 {
				t.Fatalf("legal next stage: evidence=%d version delta=%d calls=%d; want 2, 2, 2", len(evidence), followUp.Task.Version-before.Version, port.callCount())
			}
		})
	}
}
