// Copyright 2014-2021 Aerospike, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package aerospike

import (
	"time"

	"github.com/aerospike/aerospike-client-go/internal/atomic"
)

func (clnt *Client) scanPartitions(policy *ScanPolicy, tracker *partitionTracker, namespace string, setName string, recordset *Recordset, binNames ...string) {
	defer recordset.signalEnd()

	// for exponential backoff
	interval := policy.SleepBetweenRetries

	var errs Error
	for {
		list, err := tracker.assignPartitionsToNodes(clnt.Cluster(), namespace)
		if err != nil {
			errs = chainErrors(err, errs)
			recordset.sendError(errs)
			return
		}

		maxConcurrentNodes := policy.MaxConcurrentNodes
		if maxConcurrentNodes <= 0 {
			maxConcurrentNodes = len(list)
		}

		if !policy.ConcurrentNodes {
			maxConcurrentNodes = 1
		}

		weg := newWeightedErrGroup(maxConcurrentNodes)
		for _, nodePartition := range list {
			cmd := newScanPartitionCommand(policy, tracker, nodePartition, namespace, setName, binNames, recordset)
			weg.execute(cmd)
		}
		// no need to manage the errors; they are send back via the recordset
		weg.wait()

		if done, err := tracker.isComplete(&policy.BasePolicy); done || err != nil {
			// Scan is complete.
			if err != nil {
				errs = chainErrors(err, errs)
				recordset.sendError(errs)
			}
			return
		}

		if policy.SleepBetweenRetries > 0 {
			// Sleep before trying again.
			time.Sleep(interval)

			if policy.SleepMultiplier > 1 {
				interval = time.Duration(float64(interval) * policy.SleepMultiplier)
			}
		}

		recordset.resetTaskID()
	}

}

func (clnt *Client) scanNodes(policy *ScanPolicy, recordset *Recordset, clusterKey int64, namespace, setName string, binNames []string, nodes ...*Node) {
	maxConcurrentNodes := policy.MaxConcurrentNodes
	if maxConcurrentNodes <= 0 {
		maxConcurrentNodes = len(nodes)
	}

	if !policy.ConcurrentNodes {
		maxConcurrentNodes = 1
	}

	weg := newWeightedErrGroup(maxConcurrentNodes)
	first := atomic.NewBool(true)
	for _, node := range nodes {
		cmd := newScanCommand(node, policy, namespace, setName, binNames, recordset, clusterKey, first.CompareAndToggle(true))
		weg.executeFunc(cmd, func() { recordset.signalEnd() })
	}

	// skip the wg.wait, no need to sync here; Recordset will do the sync
	// wg.wait
}
