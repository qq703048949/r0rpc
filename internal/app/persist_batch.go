package app

import (
	"context"
	"fmt"
	"r0rpc/internal/model"
	"r0rpc/internal/store"
)

type rpcMetricBatchKey struct {
	StatDate   string
	ClientID   string
	GroupName  string
	ActionName string
}

type deviceMetricBatchKey struct {
	StatDate  string
	ClientID  string
	GroupName string
}

type metricAccumulator interface {
	AddTotals(totalRequests, successRequests, failedRequests, timeoutRequests, totalLatencyMS, maxLatencyMS int64)
}

func accumulateMetric(target metricAccumulator, status string, latencyMS int64) {
	var success int64
	var failed int64
	var timeoutCount int64

	switch status {
	case "success":
		success = 1
	case "timeout":
		timeoutCount = 1
	default:
		failed = 1
	}

	target.AddTotals(1, success, failed, timeoutCount, latencyMS, latencyMS)
}

func (a *App) runPersistBatch(tasks []persistTask) error {
	if len(tasks) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), persistTaskTimeout)
	defer cancel()

	completed := make([]*model.RPCRequest, 0, len(tasks))
	rpcMetrics := make(map[rpcMetricBatchKey]*store.RPCDailyMetricDelta, len(tasks))
	deviceMetrics := make(map[deviceMetricBatchKey]*store.DeviceDailyMetricDelta, len(tasks))

	for _, task := range tasks {
		switch task.Kind {
		case persistTaskCreateRequest:
			if err := a.runPersistTask(task); err != nil {
				return err
			}
		case persistTaskCompleteRequest:
			var requesterUserID *int64
			if task.HasRequesterUserID {
				requesterUserID = &task.RequesterUserID
			}
			completed = append(completed, &model.RPCRequest{
				RequestID:           task.RequestID,
				GroupName:           task.GroupName,
				ActionName:          task.ActionName,
				ClientID:            task.ClientID,
				RequesterUserID:     requesterUserID,
				RequestPayloadJSON:  task.RequestPayload,
				ResponsePayloadJSON: task.ResponsePayload,
				Status:              task.Status,
				HTTPCode:            task.HTTPCode,
				LatencyMS:           task.LatencyMS,
				ErrorMessage:        task.ErrorMessage,
			})
		case persistTaskRPCMetric:
			dateKey := task.StatTime.Format("2006-01-02")
			key := rpcMetricBatchKey{StatDate: dateKey, ClientID: task.ClientID, GroupName: task.GroupName, ActionName: task.ActionName}
			delta := rpcMetrics[key]
			if delta == nil {
				delta = &store.RPCDailyMetricDelta{StatDate: dateKey, ClientID: task.ClientID, GroupName: task.GroupName, ActionName: task.ActionName}
				rpcMetrics[key] = delta
			}
			accumulateMetric(delta, task.Status, task.LatencyMS)
		case persistTaskDeviceMetric:
			dateKey := task.StatTime.Format("2006-01-02")
			key := deviceMetricBatchKey{StatDate: dateKey, ClientID: task.ClientID, GroupName: task.GroupName}
			delta := deviceMetrics[key]
			if delta == nil {
				delta = &store.DeviceDailyMetricDelta{StatDate: dateKey, ClientID: task.ClientID, GroupName: task.GroupName}
				deviceMetrics[key] = delta
			}
			accumulateMetric(delta, task.Status, task.LatencyMS)
		default:
			return fmt.Errorf("unknown persist task kind: %s", task.Kind)
		}
	}

	if len(completed) > 0 {
		if err := a.Store.CompleteRPCRequests(ctx, completed); err != nil {
			return err
		}
	}
	if len(rpcMetrics) > 0 {
		items := make([]store.RPCDailyMetricDelta, 0, len(rpcMetrics))
		for _, item := range rpcMetrics {
			items = append(items, *item)
		}
		if err := a.Store.IncrementRPCDailyMetricsBatch(ctx, items); err != nil {
			return err
		}
	}
	if len(deviceMetrics) > 0 {
		items := make([]store.DeviceDailyMetricDelta, 0, len(deviceMetrics))
		for _, item := range deviceMetrics {
			items = append(items, *item)
		}
		if err := a.Store.IncrementDeviceDailyMetricsBatch(ctx, items); err != nil {
			return err
		}
	}

	return nil
}
