package replay

import (
	"context"
	"errors"
	"log"
	"syscall"
	"time"

	"gem/internal/ai"
	"gem/internal/store"
)

func processCPU() time.Duration {
	var usage syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &usage)
	return time.Duration(usage.Utime.Sec+usage.Stime.Sec)*time.Second + time.Duration(usage.Utime.Usec+usage.Stime.Usec)*time.Microsecond
}

func RunWorker(ctx context.Context, st *store.Store, cpuBudget time.Duration) error {
	path, err := st.ReplayLockPath()
	if err != nil {
		return err
	}
	singleton, err := tryAcquire(path + ".worker")
	if err != nil {
		return err
	}
	defer singleton.Close()
	if err := st.BackfillReplayJobs(ctx); err != nil {
		return err
	}
	log.Printf("replay worker ready: one job, search CPU budget %s; CPU cap requires systemd unit", cpuBudget)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		if ctx.Err() != nil {
			return nil
		}
		idle, err := st.ReplayIdle(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			return err
		}
		if idle {
			worked, err := workNext(ctx, st, path, cpuBudget)
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, store.ErrReplayLease) {
				log.Printf("replay worker: %v", err)
			}
			if worked {
				continue
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func workNext(ctx context.Context, st *store.Store, path string, cpuBudget time.Duration) (bool, error) {
	lock, err := tryAcquire(path)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer lock.Close()
	owner := newOwner()
	lease, err := st.BeginReplayExecution(ctx, owner, true)
	if err != nil {
		return false, err
	}
	defer endExecution(st, lease)
	job, err := st.ClaimReplayJob(ctx, owner)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	jobCtx, cancel := context.WithCancel(ctx)
	done := heartbeat(jobCtx, cancel, func(ctx context.Context) error {
		if err := st.RenewReplayExecution(ctx, lease); err != nil {
			return err
		}
		return st.RenewReplayJob(ctx, job)
	})
	err = runJob(jobCtx, st, job, lease, cpuBudget)
	cancel()
	<-done
	cleanup, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if finishErr := st.FinishReplayJob(cleanup, job, err == nil, err); finishErr != nil {
		return true, finishErr
	}
	return true, err
}

func runJob(ctx context.Context, st *store.Store, job store.ReplayJob, lease store.ReplayLease, cpuBudget time.Duration) error {
	g, err := st.Game(ctx, job.GameID)
	if err != nil {
		return err
	}
	if g.Status != "finished" {
		return ErrPlaying
	}
	rp, err := Load(ctx, st, g)
	if err != nil {
		return err
	}
	for _, f := range rp.Frames {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !f.Main || (f.Analysis != nil && f.Review != nil) {
			continue
		}
		if err := st.RenewReplayExecution(ctx, lease); err != nil {
			return err
		}
		coach := ai.NewMaster(cpuBudget)
		coach.Workers = 1
		startCPU := processCPU()
		coach.SearchExpired = func() bool { return processCPU()-startCPU >= cpuBudget }
		handCtx, stop := context.WithTimeout(ctx, 3*time.Minute)
		_, err := analyze(handCtx, st, job.GameID, f.Seq, coach, lease)
		stop()
		if err != nil {
			return err
		}
	}
	return nil
}
