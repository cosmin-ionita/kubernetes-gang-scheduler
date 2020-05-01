package main

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"time"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	SCHEDULER_NAME = "gang-scheduler"

	NODE_COST_LABEL_KEY         = "cost"
	POD_ROLE_LABEL_KEY          = "spark-role"
	DRIVER_POD_LABEL_VALUE      = "driver"
	DRIVER_CPU_LABEL_KEY        = "driver-cpu"
	DRIVER_MEM_LABEL_KEY        = "driver-mem"
	DRIVER_EXEC_COUNT_LABEL_KEY = "executors"
	DRIVER_EXEC_CPU_LABEL_KEY   = "exec-cpu"
	DRIVER_EXEC_MEM_LABEL_KEY   = "exec-mem"

	EXECUTOR_POD_LABEL_VALUE = "executor"
)

type Scheduler struct {
	clientset  *kubernetes.Clientset
	podQueue   chan *v1.Pod
	nodeLister listersv1.NodeLister
}

type Binding struct {
	pod      *v1.Pod
	nodeName string
}

type Event struct {
	pod     *v1.Pod
	message string
}

var (
	nodeUsageCache nodeCache

	eventsQueue  = make(chan Event, 300)
	bindingQueue = make(chan Binding, 300)
)

func NewScheduler(podQueue chan *v1.Pod, quit chan struct{}) Scheduler {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatal(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	return Scheduler{
		clientset:  clientset,
		podQueue:   podQueue,
		nodeLister: initInformers(clientset, podQueue, quit),
	}
}

func initInformers(clientset *kubernetes.Clientset, podQueue chan *v1.Pod, quit chan struct{}) listersv1.NodeLister {
	factory := informers.NewSharedInformerFactory(clientset, 0)

	nodeInformer := factory.Core().V1().Nodes()
	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			node, ok := obj.(*v1.Node)
			if !ok {
				log.Println("this is not a node")
				return
			}
			log.Printf("New Node Added to Store: %s", node.GetName())
		},
	})

	podInformer := factory.Core().V1().Pods()
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod, ok := obj.(*v1.Pod)
			if !ok {
				log.Println("this is not a pod")
				return
			}
			if pod.Spec.NodeName == "" && pod.Spec.SchedulerName == SCHEDULER_NAME {
				podQueue <- pod
			}
		},
		DeleteFunc: func(obj interface{}) {
			pod, ok := obj.(*v1.Pod)
			if !ok {
				log.Println("this is not a pod")
				return
			}
			log.Println("Delete / terminate pod:  ", pod.Name, "/", pod.Namespace, "/", pod.Spec.NodeName)
		},
	})

	factory.Start(quit)
	return nodeInformer.Lister()
}

func main() {
	log.Println("Gang scheduler launched!")

	rand.Seed(time.Now().Unix())

	podQueue := make(chan *v1.Pod, 300)
	defer close(podQueue)

	quit := make(chan struct{})
	defer close(quit)

	scheduler := NewScheduler(podQueue, quit)
	nodeUsageCache = createNodeCache(scheduler.nodeLister)

	go scheduler.bindProcess()
	go scheduler.eventEmitterProcess()

	scheduler.Run(quit)
}

func (s *Scheduler) bindProcess() {
	for {
		binding := <-bindingQueue
		log.Println("Binding pod: ", binding.pod.Name, " to node: ", binding.nodeName)
		err := s.bindPod(binding.pod, binding.nodeName)

		if err != nil {
			log.Println("Bind error: ", err.Error())
		}
	}
}

func (s *Scheduler) eventEmitterProcess() {
	for {
		event := <-eventsQueue
		log.Println("Sending event for pod: ", event.pod.Name, " with message: ", event.message)
		err := s.emitEvent(event.pod, event.message)

		if err != nil {
			log.Println("Event emitting error: ", err.Error())
		}
	}
}

func (s *Scheduler) Run(quit chan struct{}) {
	wait.Until(s.ScheduleCycle, 0, quit)
}

func (s *Scheduler) ScheduleCycle() {

	pod := <-s.podQueue
	log.Println("Attempting to schedule the pod: ", pod.Namespace, "/", pod.Name)

	start := time.Now()

	node, err := s.chooseNode(pod)
	if err != nil {
		log.Println("Cannot find node that fits pod", err.Error())
		return
	}

	bindingQueue <- Binding{
		pod:      pod,
		nodeName: node,
	}

	message := fmt.Sprintf("Placed pod [%s/%s] on %s\n", pod.Namespace, pod.Name, node)

	eventsQueue <- Event{
		pod:     pod,
		message: message,
	}

	elapsed := time.Since(start)

	log.Println("Scheduling time in nanoseconds: ", elapsed.Nanoseconds())
}

func (s *Scheduler) chooseNode(pod *v1.Pod) (string, error) {

	if pod.Labels[POD_ROLE_LABEL_KEY] == DRIVER_POD_LABEL_VALUE {
		if s.driverFits(pod) && s.executorsFit(pod) {
			return nodeUsageCache.schedulePod(pod.Labels[DRIVER_CPU_LABEL_KEY], pod.Labels[DRIVER_MEM_LABEL_KEY]), nil
		} else {
			log.Println("The spark job does not fit into the cluster")
			return "", errors.New("The spark job does not fit into the cluster")
		}
	} else if pod.Labels[POD_ROLE_LABEL_KEY] == EXECUTOR_POD_LABEL_VALUE {
		return nodeUsageCache.schedulePod(pod.Labels[DRIVER_EXEC_CPU_LABEL_KEY], pod.Labels[DRIVER_EXEC_MEM_LABEL_KEY]), nil
	}

	return "", errors.New("It shouldn't reach this")
}

func (s *Scheduler) driverFits(pod *v1.Pod) bool {
	return nodeUsageCache.podFits(pod.Labels[DRIVER_CPU_LABEL_KEY], pod.Labels[DRIVER_MEM_LABEL_KEY])
}

func (s *Scheduler) executorsFit(pod *v1.Pod) bool {
	return nodeUsageCache.multiplePodsFit(
		pod.Labels[DRIVER_EXEC_COUNT_LABEL_KEY],
		pod.Labels[DRIVER_EXEC_CPU_LABEL_KEY],
		pod.Labels[DRIVER_EXEC_MEM_LABEL_KEY])
}

func (s *Scheduler) bindPod(p *v1.Pod, node string) error {
	return s.clientset.CoreV1().Pods(p.Namespace).Bind(&v1.Binding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.Name,
			Namespace: p.Namespace,
		},
		Target: v1.ObjectReference{
			APIVersion: "v1",
			Kind:       "Node",
			Name:       node,
		},
	})
}

func (s *Scheduler) emitEvent(p *v1.Pod, message string) error {
	timestamp := time.Now().UTC()
	_, err := s.clientset.CoreV1().Events(p.Namespace).Create(&v1.Event{
		Count:          1,
		Message:        message,
		Reason:         "Scheduled",
		LastTimestamp:  metav1.NewTime(timestamp),
		FirstTimestamp: metav1.NewTime(timestamp),
		Type:           "Normal",
		Source: v1.EventSource{
			Component: SCHEDULER_NAME,
		},
		InvolvedObject: v1.ObjectReference{
			Kind:      "Pod",
			Name:      p.Name,
			Namespace: p.Namespace,
			UID:       p.UID,
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: p.Name + "-",
		},
	})
	if err != nil {
		return err
	}
	return nil
}

// TODO -> those need to be fixed in the future

//schedulingMeter = promauto.NewHistogram(prometheus.HistogramOpts{
//	Name:    "scheduling_time",
//	Help:    "This metric shows the scheduling time",
//	Buckets: prometheus.DefBuckets,
//})

//func powerPromServer() {
//	http.Handle("/metrics", promhttp.Handler())
//	err := http.ListenAndServe(":2112", nil)
//
//	if err != nil {
//		fmt.Println("Got an error when powering up the Prom server: ", err)
//	}
//}