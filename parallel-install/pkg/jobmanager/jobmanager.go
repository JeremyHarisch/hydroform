package jobmanager

import (
	"context"
	"sync"
	"time"

	"github.com/kyma-incubator/hydroform/parallel-install/pkg/config"
	"github.com/kyma-incubator/hydroform/parallel-install/pkg/logger"
	"k8s.io/client-go/kubernetes"
)

type component string
type executionTime int
type jobName string

type jobStatus struct {
	job    jobName
	status bool
	err    error
}

type job interface {
	execute(*config.Config, kubernetes.Interface, context.Context) error
	when() (component, executionTime)
	identify() jobName
}

const (
	Pre executionTime = iota
	Post
)

var duration time.Duration

var preJobMap = make(map[component][]job)
var postJobMap = make(map[component][]job)
var finishedJobs = []jobStatus{}

var kubeClient kubernetes.Interface
var cfg *config.Config

var log logger.Interface

// Register job
func register(j job) int {
	component, executionTime := j.when()
	if executionTime == Pre {
		preJobMap[component] = append(preJobMap[component], j)
	} else if executionTime == Post {
		postJobMap[component] = append(postJobMap[component], j)
	}
	return 0
}

// Sets Installation Config at package level
func SetConfig(config *config.Config) {
	cfg = config
}

// Sets Kubernetes Cleint at package level
func SetKubeClient(kc kubernetes.Interface) {
	kubeClient = kc
}

// Sets Logger at package level
func SetLogger(logClient logger.Interface) {
	log = logClient
}

// Function should be called before component is being deployed/upgraded
// If the Context is cancelled, the worker quits immediately, skipping the remaining components
func ExecutePre(ctx context.Context, c string) {
	execute(ctx, c, preJobMap)
}

// Function should be called after compoent is being deployed/upgraded
// If the Context is cancelled, the worker quits immediately, skipping the remaining components
func ExecutePost(ctx context.Context, c string) {
	execute(ctx, c, postJobMap)
}

// Used by ExecutePre() && ExecutePost()
// Used to start workers and grab jobs belonging to the respective component
func execute(ctx context.Context, c string, executionMap map[component][]job) {
	var wg sync.WaitGroup

	start := time.Now()

	jobs := executionMap[component(c)]
	statusChan := make(chan jobStatus, len(jobs))
	wg.Add(len(jobs))

	if len(jobs) > 0 {
		for _, job := range jobs {
			go worker(ctx, statusChan, &wg, job)
		}
	}
	go func() {
		wg.Wait()
		close(statusChan)
	}()

	emptyJob := jobStatus{}
	for status := range statusChan {
		log.Infof("Job Status: %v", status)
		if status != emptyJob {
			finishedJobs = append(finishedJobs, status)
			if status.status == true {
				log.Infof("Following job executed: %v", status.job)
			} else if status.status == false {
				log.Infof("Following job failed while execution: `%v` with error: %s", status.job, status.err)
			}
		}
	}

	t := time.Now()
	duration += t.Sub(start)
}

func worker(ctx context.Context, statusChan chan<- jobStatus, wg *sync.WaitGroup, j job) {
	defer wg.Done()
	if err := j.execute(cfg, kubeClient, ctx); err != nil {
		j := jobStatus{j.identify(), false, err}
		statusChan <- j
	} else {
		j := jobStatus{j.identify(), true, nil}
		statusChan <- j
	}
}

// Returns duration of all jobs for benchmarking
func GetDuration() time.Duration {
	ret := duration
	resetDuration()
	return ret
}

func resetDuration() {
	duration = 0 * time.Microsecond
}

func resetMap(exec executionTime) {
	if exec == Pre {
		preJobMap = make(map[component][]job)
	} else if exec == Post {
		postJobMap = make(map[component][]job)
	}
}

func resetFinishedJobsMap() {
	finishedJobs = []jobStatus{}
}

func init() {
	duration = 0 * time.Microsecond
}