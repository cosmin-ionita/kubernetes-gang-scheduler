# Simple and fast, fully event-based Kubernetes Gang-Scheduler

This is a very simple implementation of a Gang Scheduler for Kubernetes, built mainly for research purposes. **It needs to be revised before being used in production environments**.

## Background

This scheduler was designed mainly for running Spark jobs with Kubernetes as a resource manager. 

The default Kubernetes scheduler was not designed to be aware of connections between pods (like there is between a driver and an executor), so if it's used to schedule
a Spark job, it can half-schedule jobs if the cluster is running at capacity, thus leading to the inability for a job to make progress (because it's not fully scheduled).

To avoid that issue, this scheduler was designed. It's main purpose is to schedule a driver pod only if there is enough room in the cluster for all the executors associated to that Job.

## Scheduling workflow

The scheduler has an asynchronously-updated internal resourceCache, which keeps the state
of the cluster (from node resources standpoint). It's basically a map from nodeName to it's resources characteristics. 

1. Using a pod informer, it takes the new pod that's being scheduled using this particular scheduler
2. Check if that pod is a driver or an executor (by looking at it's labels)

3. If it's a driver, check if all it's executors fit in the resourceCache. If so, schedule the driver pod.

4. If it's an executor, schedule it directly

5. For any scheduled pod, launch a bind event (async) and a cluster event

## Acknowledgments

* This scheduler was designed to be ran on a cluster that is only used for running Spark jobs
* It does not have features like preemption or volumes-binding, for performance reasons.
* It's main purpose is to make sure the jobs are scheduled in a gang-way
