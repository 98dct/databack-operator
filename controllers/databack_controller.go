/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io/ioutil"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"os"
	"os/exec"
	"reflect"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"strconv"
	"strings"
	"sync"
	"time"

	operatordctcomv1beta1 "databack-operator/api/v1beta1"
)

var logger *zap.Logger

func init() {
	cfg := zap.Config{
		Encoding:         "console",
		Level:            zap.NewAtomicLevelAt(zapcore.InfoLevel),
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder   // 设置时间格式
	cfg.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder // 显示短路径的调用者信息

	zaplogger, err := cfg.Build()
	if err != nil {
		panic(err)
	}
	logger = zaplogger
}

// DatabackReconciler reconciles a Databack object
type DatabackReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	BackupQueue map[string]operatordctcomv1beta1.Databack
	Wg          sync.WaitGroup
	Tickers     []*time.Ticker
	Lock        sync.RWMutex
}

//+kubebuilder:rbac:groups=operator.dct.com,resources=databacks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=operator.dct.com,resources=databacks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=operator.dct.com,resources=databacks/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Databack object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.2/pkg/reconcile
func (r *DatabackReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here
	//查询资源是否存在 不存在 说明资源不存在
	var databackK8s operatordctcomv1beta1.Databack
	err := r.Client.Get(ctx, req.NamespacedName, &databackK8s)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Sugar().Infof("%s 停止", req.Name)
			//资源不存在说明被删除了，执行删除逻辑
			r.DeleteQueue(databackK8s)
		}
		logger.Sugar().Infof("%s 异常 %v", req.Name, err)
		return ctrl.Result{}, err
	}

	if lastDataback, ok := r.BackupQueue[databackK8s.Name]; ok {
		if equal := reflect.DeepEqual(databackK8s.Spec, lastDataback.Spec); equal {
			return ctrl.Result{}, err
		}
	}

	//create/update logic
	r.AddQueue(databackK8s)
	return ctrl.Result{}, nil
}

func (r *DatabackReconciler) AddQueue(databack operatordctcomv1beta1.Databack) {
	if r.BackupQueue == nil {
		r.BackupQueue = make(map[string]operatordctcomv1beta1.Databack)
	}
	r.BackupQueue[databack.Name] = databack
	r.StopLoop()
	go r.RunLoop()
}

func (r *DatabackReconciler) DeleteQueue(databack operatordctcomv1beta1.Databack) {
	delete(r.BackupQueue, databack.Name)
	r.StopLoop()
	go r.RunLoop()
}

// 停止循环
func (r *DatabackReconciler) StopLoop() {
	for _, ticker := range r.Tickers {
		if ticker != nil {
			ticker.Stop()
		}
	}
}

// 开始循环
func (r *DatabackReconciler) RunLoop() {
	for name, databack := range r.BackupQueue {

		if !databack.Spec.Enable {
			logger.Sugar().Infof("%s 未开启", name)
			databack.Status.Active = false
			r.UpdateStatus(databack)
			continue
		}

		delay := r.getDelaySeconds(databack.Spec.StartTime)
		if delay.Hours() < 1 {
			logger.Sugar().Infof("%s 将在 %.1f分钟后执行", name, delay.Minutes())
		} else {
			logger.Sugar().Infof("%s 将在 %.1f小时后执行", name, delay.Hours())
		}
		databack.Status.Active = true
		nextTime := r.getNextTime(delay.Seconds())
		databack.Status.NexTime = nextTime.Unix()
		r.UpdateStatus(databack)

		ticker := time.NewTicker(delay)
		r.Tickers = append(r.Tickers, ticker)
		r.Wg.Add(1)
		go func(databack operatordctcomv1beta1.Databack) {
			defer r.Wg.Done()
			for {
				<-ticker.C
				//重置ticker

				ticker.Reset(time.Duration(databack.Spec.Period) * time.Minute)
				logger.Sugar().Infof("%s 将在 %d分钟后循环执行", databack.Name, databack.Spec.Period)
				databack.Status.Active = true
				databack.Status.NexTime = r.getNextTime(float64(databack.Spec.Period) * 60).Unix()

				//备份任务
				err := r.DumpAndUploadOss(databack)
				if err != nil {
					logger.Sugar().Errorf("%s 同步数据报错 %v", databack.Name, err)
					databack.Status.LastBackupResult = fmt.Sprintf("databack failed %v", err)
				} else {
					logger.Sugar().Infof("%s 同步数据成功", databack.Name)
					databack.Status.LastBackupResult = "databack success"
				}
				//更新备份状态
				r.UpdateStatus(databack)
			}
		}(databack)
	}

	r.Wg.Wait()
}

// GetDelaySeconds 获取第一次启动的延时时间(秒)
func (r *DatabackReconciler) getDelaySeconds(startTime string) time.Duration {
	//计算小时和分钟  "04:14"
	times := strings.Split(startTime, ":")
	expectedHour, _ := strconv.Atoi(times[0]) //04
	expectedMin, _ := strconv.Atoi(times[1])  //14
	now := time.Now().Truncate(time.Second)   //"2023-12-11 04:12:00"
	//开始时间是，startTime那一天的0时0分0秒
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	//结束时间是 startTime后一天的0时0分0秒
	todayEnd := todayStart.Add(24 * time.Hour)
	var seconds int
	//期望时间点 一天的秒总数
	//4h14m0s
	expectedDuration := time.Hour*time.Duration(expectedHour) + time.Minute*time.Duration(expectedMin)
	//当前时间点 一天的秒总数
	//4h12m0s
	curDuration := time.Hour*time.Duration(now.Hour()) + time.Minute*time.Duration(now.Minute())
	//当前时间已经过了预定时间 明天执行
	if curDuration >= expectedDuration {
		seconds = int(todayEnd.Add(expectedDuration).Sub(now).Seconds())
	} else {
		//今天执行
		seconds = int(todayStart.Add(expectedDuration).Sub(now).Seconds())
	}
	return time.Second * time.Duration(seconds)
}

// 当前时间x秒后的时间
// 返回标准时间格式
func (r *DatabackReconciler) getNextTime(seconds float64) time.Time {
	currentTime := time.Now()
	return currentTime.Add(time.Second * time.Duration(seconds))
}

func (r *DatabackReconciler) UpdateStatus(backup operatordctcomv1beta1.Databack) {
	r.Lock.Lock()
	defer r.Lock.Unlock()
	ctx := context.TODO()
	namespacedName := types.NamespacedName{
		Name:      backup.Name,
		Namespace: backup.Namespace,
	}
	var dataBackupK8s operatordctcomv1beta1.Databack
	err := r.Get(ctx, namespacedName, &dataBackupK8s)
	if err != nil {
		logger.Sugar().Error(err)
		return
	}
	//状态更新为激活
	dataBackupK8s.Status = backup.Status
	err = r.Client.Status().Update(ctx, &dataBackupK8s)
	if err != nil {
		logger.Sugar().Error(err)
		return
	}
}

// mysql数据导出+数据上报到OSS
func (r *DatabackReconciler) DumpAndUploadOss(backup operatordctcomv1beta1.Databack) error {
	defer func() {
		if err := recover(); err != nil {
			logger.Sugar().Errorf("run time panic: %v", err)
		}
	}()
	//dump
	mysqlHost := backup.Spec.Origin.Host
	mysqlPort := backup.Spec.Origin.Port
	mysqlUsername := backup.Spec.Origin.Username
	mysqlPassword := backup.Spec.Origin.Password
	now := time.Now()
	backupDate := fmt.Sprintf("%02d-%02d", now.Month(), now.Day())
	folderPath := fmt.Sprintf("/tmp/%s/%s/", backup.Name, backupDate)
	//创建文件夹
	if _, err := os.Stat(folderPath); err != nil {
		if errx := os.MkdirAll(folderPath, 0700); errx == nil {
			logger.Sugar().Infof("created dir %s", folderPath)
		} else {
			logger.Sugar().Errorf("%s ：%v", backup.Name, errx)
			return errx
		}
	}
	//计算当天同步的文件个数
	files, err := ioutil.ReadDir(folderPath)
	if err != nil {
		logger.Sugar().Errorf("%s ：%v", backup.Name, err)
		return err
	}
	number := len(files) + 1
	filename := fmt.Sprintf("%s#%d.sql", folderPath, number)
	dumpCmd := fmt.Sprintf("mysqldump -h%s -P%d -u%s -p%s --all-databases > %s",
		mysqlHost, mysqlPort, mysqlUsername, mysqlPassword, filename)
	logger.Sugar().Infof("%s %s", backup.Name, dumpCmd)
	command := exec.Command("bash", "-c", dumpCmd)
	_, err = command.Output() // 执行命令并获取输出
	if err != nil {
		logger.Sugar().Errorf("%s %v", backup.Name, err)
		return err
	}
	//upload
	endpoint := backup.Spec.Destination.EndPoint
	accessKey := backup.Spec.Destination.AccessKey
	asscessSecret := backup.Spec.Destination.AccessSecret
	bucketName := backup.Spec.Destination.BucketName
	// Initialize minio client object.
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, asscessSecret, ""),
		Secure: false,
	})
	if err != nil {
		logger.Sugar().Infof("%s %v", backup.Name, err)
		return err
	}
	object, err := os.Open(filename)
	if err != nil {
		logger.Sugar().Infof("%s %v", backup.Name, err)
		return err
	}
	ctx := context.TODO()
	_, err = minioClient.PutObject(ctx, bucketName, filename, object, -1, minio.PutObjectOptions{})
	if err != nil {
		logger.Sugar().Infof("%s %v", backup.Name, err)
		return err
	}
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatabackReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&operatordctcomv1beta1.Databack{}).
		Complete(r)
}
