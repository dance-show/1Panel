package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/1Panel-dev/1Panel/backend/app/dto"
	"github.com/1Panel-dev/1Panel/backend/app/model"
	"github.com/1Panel-dev/1Panel/backend/buserr"
	"github.com/1Panel-dev/1Panel/backend/constant"
	"github.com/1Panel-dev/1Panel/backend/global"
	"github.com/1Panel-dev/1Panel/backend/utils/cmd"
	"github.com/1Panel-dev/1Panel/backend/utils/common"
	"github.com/1Panel-dev/1Panel/backend/utils/compose"
	"github.com/1Panel-dev/1Panel/backend/utils/mysql"
	"github.com/1Panel-dev/1Panel/backend/utils/mysql/client"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jinzhu/copier"
	"github.com/pkg/errors"
)

type MysqlService struct{}

type IMysqlService interface {
	SearchWithPage(search dto.MysqlDBSearch) (int64, interface{}, error)
	ListDBOption() ([]dto.MysqlOption, error)
	Create(ctx context.Context, req dto.MysqlDBCreate) (*model.DatabaseMysql, error)
	LoadFromRemote(from string) error
	ChangeAccess(info dto.ChangeDBInfo) error
	ChangePassword(info dto.ChangeDBInfo) error
	UpdateVariables(updates []dto.MysqlVariablesUpdate) error
	UpdateConfByFile(info dto.MysqlConfUpdateByFile) error
	UpdateDescription(req dto.UpdateDescription) error
	DeleteCheck(id uint) ([]string, error)
	Delete(ctx context.Context, req dto.MysqlDBDelete) error
	LoadStatus() (*dto.MysqlStatus, error)
	LoadVariables() (*dto.MysqlVariables, error)
	LoadBaseInfo() (*dto.DBBaseInfo, error)
	LoadRemoteAccess() (bool, error)

	LoadDatabaseFile(req dto.OperationWithNameAndType) (string, error)
}

func NewIMysqlService() IMysqlService {
	return &MysqlService{}
}

func (u *MysqlService) SearchWithPage(search dto.MysqlDBSearch) (int64, interface{}, error) {
	total, mysqls, err := mysqlRepo.Page(search.Page, search.PageSize,
		mysqlRepo.WithByFrom(search.From),
		commonRepo.WithLikeName(search.Info),
		commonRepo.WithOrderRuleBy(search.OrderBy, search.Order),
	)
	var dtoMysqls []dto.MysqlDBInfo
	for _, mysql := range mysqls {
		var item dto.MysqlDBInfo
		if err := copier.Copy(&item, &mysql); err != nil {
			return 0, nil, errors.WithMessage(constant.ErrStructTransform, err.Error())
		}
		dtoMysqls = append(dtoMysqls, item)
	}
	return total, dtoMysqls, err
}

func (u *MysqlService) ListDBOption() ([]dto.MysqlOption, error) {
	mysqls, err := mysqlRepo.List()
	var dbs []dto.MysqlOption
	for _, mysql := range mysqls {
		var item dto.MysqlOption
		if err := copier.Copy(&item, &mysql); err != nil {
			return nil, errors.WithMessage(constant.ErrStructTransform, err.Error())
		}
		dbs = append(dbs, item)
	}
	return dbs, err
}

func (u *MysqlService) Create(ctx context.Context, req dto.MysqlDBCreate) (*model.DatabaseMysql, error) {
	if cmd.CheckIllegal(req.Name, req.Username, req.Password, req.Format, req.Permission) {
		return nil, buserr.New(constant.ErrCmdIllegal)
	}

	mysql, _ := mysqlRepo.Get(commonRepo.WithByName(req.Name), remoteDBRepo.WithByFrom(req.From))
	if mysql.ID != 0 {
		return nil, constant.ErrRecordExist
	}

	var createItem model.DatabaseMysql
	if err := copier.Copy(&createItem, &req); err != nil {
		return nil, errors.WithMessage(constant.ErrStructTransform, err.Error())
	}

	if req.From == "local" && req.Username == "root" {
		return nil, errors.New("Cannot set root as user name")
	}

	cli, version, err := LoadMysqlClientByFrom(req.From)
	if err != nil {
		return nil, err
	}

	if req.From == "local" {
		app, err := appInstallRepo.LoadBaseInfo("mysql", "")
		if err != nil {
			return nil, err
		}
		createItem.MysqlName = app.Name
	} else {
		createItem.MysqlName = req.From
	}
	defer cli.Close()
	if err := cli.Create(client.CreateInfo{
		Name:       req.Name,
		Format:     req.Format,
		Username:   req.Username,
		Password:   req.Password,
		Permission: req.Permission,
		Version:    version,
		Timeout:    300,
	}); err != nil {
		return nil, err
	}

	global.LOG.Infof("create database %s successful!", req.Name)
	if err := mysqlRepo.Create(ctx, &createItem); err != nil {
		return nil, err
	}
	return &createItem, nil
}

func (u *MysqlService) LoadFromRemote(from string) error {
	client, version, err := LoadMysqlClientByFrom(from)
	if err != nil {
		return err
	}

	mysqlName := from
	if from == "local" {
		app, err := appInstallRepo.LoadBaseInfo("mysql", "")
		if err != nil {
			return err
		}
		mysqlName = app.Name
	}

	databases, err := mysqlRepo.List(remoteDBRepo.WithByFrom(from))
	if err != nil {
		return err
	}
	datas, err := client.SyncDB(version)
	if err != nil {
		return err
	}
	for _, data := range datas {
		hasOld := false
		for _, oldData := range databases {
			if strings.EqualFold(oldData.Name, data.Name) {
				hasOld = true
				break
			}
		}
		if !hasOld {
			var createItem model.DatabaseMysql
			if err := copier.Copy(&createItem, &data); err != nil {
				return errors.WithMessage(constant.ErrStructTransform, err.Error())
			}
			createItem.MysqlName = mysqlName
			if err := mysqlRepo.Create(context.Background(), &createItem); err != nil {
				return err
			}
		}
	}
	return nil
}

func (u *MysqlService) UpdateDescription(req dto.UpdateDescription) error {
	return mysqlRepo.Update(req.ID, map[string]interface{}{"description": req.Description})
}

func (u *MysqlService) DeleteCheck(id uint) ([]string, error) {
	var appInUsed []string
	db, err := mysqlRepo.Get(commonRepo.WithByID(id))
	if err != nil {
		return appInUsed, err
	}

	if db.From == "local" {
		app, err := appInstallRepo.LoadBaseInfo("mysql", "")
		if err != nil {
			return appInUsed, err
		}
		apps, _ := appInstallResourceRepo.GetBy(appInstallResourceRepo.WithLinkId(app.ID), appInstallResourceRepo.WithResourceId(db.ID))
		for _, app := range apps {
			appInstall, _ := appInstallRepo.GetFirst(commonRepo.WithByID(app.AppInstallId))
			if appInstall.ID != 0 {
				appInUsed = append(appInUsed, appInstall.Name)
			}
		}
	} else {
		apps, _ := appInstallResourceRepo.GetBy(appInstallResourceRepo.WithResourceId(db.ID))
		for _, app := range apps {
			appInstall, _ := appInstallRepo.GetFirst(commonRepo.WithByID(app.AppInstallId))
			if appInstall.ID != 0 {
				appInUsed = append(appInUsed, appInstall.Name)
			}
		}
	}

	return appInUsed, nil
}

func (u *MysqlService) Delete(ctx context.Context, req dto.MysqlDBDelete) error {
	db, err := mysqlRepo.Get(commonRepo.WithByID(req.ID))
	if err != nil && !req.ForceDelete {
		return err
	}
	cli, version, err := LoadMysqlClientByFrom(db.From)
	if err != nil {
		return err
	}
	defer cli.Close()
	if err := cli.Delete(client.DeleteInfo{
		Name:       db.Name,
		Version:    version,
		Username:   db.Username,
		Permission: db.Permission,
		Timeout:    300,
	}); err != nil && !req.ForceDelete {
		return err
	}

	app, err := appInstallRepo.LoadBaseInfo("mysql", "")
	if err != nil && !req.ForceDelete {
		return err
	}

	uploadDir := path.Join(global.CONF.System.BaseDir, fmt.Sprintf("1panel/uploads/database/mysql/%s/%s", app.Name, db.Name))
	if _, err := os.Stat(uploadDir); err == nil {
		_ = os.RemoveAll(uploadDir)
	}
	if req.DeleteBackup {
		localDir, err := loadLocalDir()
		if err != nil && !req.ForceDelete {
			return err
		}
		backupDir := path.Join(localDir, fmt.Sprintf("database/mysql/%s/%s", db.MysqlName, db.Name))
		if _, err := os.Stat(backupDir); err == nil {
			_ = os.RemoveAll(backupDir)
		}
		global.LOG.Infof("delete database %s-%s backups successful", app.Name, db.Name)
	}
	_ = backupRepo.DeleteRecord(ctx, commonRepo.WithByType("mysql"), commonRepo.WithByName(app.Name), backupRepo.WithByDetailName(db.Name))

	_ = mysqlRepo.Delete(ctx, commonRepo.WithByID(db.ID))
	return nil
}

func (u *MysqlService) ChangePassword(info dto.ChangeDBInfo) error {
	if cmd.CheckIllegal(info.Value) {
		return buserr.New(constant.ErrCmdIllegal)
	}
	cli, version, err := LoadMysqlClientByFrom(info.From)
	if err != nil {
		return err
	}
	defer cli.Close()
	var (
		mysqlData    model.DatabaseMysql
		passwordInfo client.PasswordChangeInfo
	)
	passwordInfo.Password = info.Value
	passwordInfo.Timeout = 300
	passwordInfo.Version = version

	if info.ID != 0 {
		mysqlData, err = mysqlRepo.Get(commonRepo.WithByID(info.ID))
		if err != nil {
			return err
		}
		passwordInfo.Name = mysqlData.Name
		passwordInfo.Username = mysqlData.Username
		passwordInfo.Permission = mysqlData.Permission
	} else {
		passwordInfo.Username = "root"
	}
	if err := cli.ChangePassword(passwordInfo); err != nil {
		return err
	}

	if info.ID != 0 {
		var appRess []model.AppInstallResource
		if info.From == "local" {
			app, err := appInstallRepo.LoadBaseInfo("mysql", "")
			if err != nil {
				return err
			}
			appRess, _ = appInstallResourceRepo.GetBy(appInstallResourceRepo.WithLinkId(app.ID), appInstallResourceRepo.WithResourceId(mysqlData.ID))
		} else {
			appRess, _ = appInstallResourceRepo.GetBy(appInstallResourceRepo.WithResourceId(mysqlData.ID))
		}
		for _, appRes := range appRess {
			appInstall, err := appInstallRepo.GetFirst(commonRepo.WithByID(appRes.AppInstallId))
			if err != nil {
				return err
			}
			appModel, err := appRepo.GetFirst(commonRepo.WithByID(appInstall.AppId))
			if err != nil {
				return err
			}

			global.LOG.Infof("start to update mysql password used by app %s-%s", appModel.Key, appInstall.Name)
			if err := updateInstallInfoInDB(appModel.Key, appInstall.Name, "user-password", true, info.Value); err != nil {
				return err
			}
		}
		global.LOG.Info("excute password change sql successful")
		_ = mysqlRepo.Update(mysqlData.ID, map[string]interface{}{"password": info.Value})
		return nil
	}

	if err := updateInstallInfoInDB("mysql", "", "password", false, info.Value); err != nil {
		return err
	}
	if err := updateInstallInfoInDB("phpmyadmin", "", "password", true, info.Value); err != nil {
		return err
	}
	return nil
}

func (u *MysqlService) ChangeAccess(info dto.ChangeDBInfo) error {
	if cmd.CheckIllegal(info.Value) {
		return buserr.New(constant.ErrCmdIllegal)
	}
	cli, version, err := LoadMysqlClientByFrom(info.From)
	if err != nil {
		return err
	}
	defer cli.Close()
	var (
		mysqlData  model.DatabaseMysql
		accessInfo client.AccessChangeInfo
	)
	accessInfo.Permission = info.Value
	accessInfo.Timeout = 300
	accessInfo.Version = version

	if info.ID != 0 {
		mysqlData, err = mysqlRepo.Get(commonRepo.WithByID(info.ID))
		if err != nil {
			return err
		}
		accessInfo.Name = mysqlData.Name
		accessInfo.Username = mysqlData.Username
		accessInfo.Password = mysqlData.Password
		accessInfo.OldPermission = mysqlData.Permission
	} else {
		accessInfo.Username = "root"
	}
	if err := cli.ChangeAccess(accessInfo); err != nil {
		return err
	}

	if mysqlData.ID != 0 {
		_ = mysqlRepo.Update(mysqlData.ID, map[string]interface{}{"permission": info.Value})
	}

	return nil
}

func (u *MysqlService) UpdateConfByFile(info dto.MysqlConfUpdateByFile) error {
	app, err := appInstallRepo.LoadBaseInfo("mysql", "")
	if err != nil {
		return err
	}
	path := fmt.Sprintf("%s/mysql/%s/conf/my.cnf", constant.AppInstallDir, app.Name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}
	defer file.Close()
	write := bufio.NewWriter(file)
	_, _ = write.WriteString(info.File)
	write.Flush()
	if _, err := compose.Restart(fmt.Sprintf("%s/mysql/%s/docker-compose.yml", constant.AppInstallDir, app.Name)); err != nil {
		return err
	}
	return nil
}

func (u *MysqlService) UpdateVariables(updates []dto.MysqlVariablesUpdate) error {
	app, err := appInstallRepo.LoadBaseInfo("mysql", "")
	if err != nil {
		return err
	}
	var files []string

	path := fmt.Sprintf("%s/mysql/%s/conf/my.cnf", constant.AppInstallDir, app.Name)
	lineBytes, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	files = strings.Split(string(lineBytes), "\n")

	group := "[mysqld]"
	for _, info := range updates {
		if !strings.HasPrefix(app.Version, "5.7") && !strings.HasPrefix(app.Version, "5.6") {
			if info.Param == "query_cache_size" {
				continue
			}
		}

		if _, ok := info.Value.(float64); ok {
			files = updateMyCnf(files, group, info.Param, common.LoadSizeUnit(info.Value.(float64)))
		} else {
			files = updateMyCnf(files, group, info.Param, info.Value)
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0666)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.WriteString(strings.Join(files, "\n"))
	if err != nil {
		return err
	}

	if _, err := compose.Restart(fmt.Sprintf("%s/mysql/%s/docker-compose.yml", constant.AppInstallDir, app.Name)); err != nil {
		return err
	}

	return nil
}

func (u *MysqlService) LoadBaseInfo() (*dto.DBBaseInfo, error) {
	var data dto.DBBaseInfo
	app, err := appInstallRepo.LoadBaseInfo("mysql", "")
	if err != nil {
		return nil, err
	}
	data.ContainerName = app.ContainerName
	data.Name = app.Name
	data.Port = int64(app.Port)

	return &data, nil
}

func (u *MysqlService) LoadRemoteAccess() (bool, error) {
	app, err := appInstallRepo.LoadBaseInfo("mysql", "")
	if err != nil {
		return false, err
	}
	hosts, err := excuteSqlForRows(app.ContainerName, app.Password, "select host from mysql.user where user='root';")
	if err != nil {
		return false, err
	}
	for _, host := range hosts {
		if host == "%" {
			return true, nil
		}
	}

	return false, nil
}

func (u *MysqlService) LoadVariables() (*dto.MysqlVariables, error) {
	app, err := appInstallRepo.LoadBaseInfo("mysql", "")
	if err != nil {
		return nil, err
	}
	variableMap, err := excuteSqlForMaps(app.ContainerName, app.Password, "show global variables;")
	if err != nil {
		return nil, err
	}
	var info dto.MysqlVariables
	arr, err := json.Marshal(variableMap)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(arr, &info)
	return &info, nil
}

func (u *MysqlService) LoadStatus() (*dto.MysqlStatus, error) {
	app, err := appInstallRepo.LoadBaseInfo("mysql", "")
	if err != nil {
		return nil, err
	}

	statusMap, err := excuteSqlForMaps(app.ContainerName, app.Password, "show global status;")
	if err != nil {
		return nil, err
	}

	var info dto.MysqlStatus
	arr, err := json.Marshal(statusMap)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(arr, &info)

	if value, ok := statusMap["Run"]; ok {
		uptime, _ := strconv.Atoi(value)
		info.Run = time.Unix(time.Now().Unix()-int64(uptime), 0).Format("2006-01-02 15:04:05")
	} else {
		if value, ok := statusMap["Uptime"]; ok {
			uptime, _ := strconv.Atoi(value)
			info.Run = time.Unix(time.Now().Unix()-int64(uptime), 0).Format("2006-01-02 15:04:05")
		}
	}

	info.File = "OFF"
	info.Position = "OFF"
	rows, err := excuteSqlForRows(app.ContainerName, app.Password, "show master status;")
	if err != nil {
		return nil, err
	}
	if len(rows) > 2 {
		itemValue := strings.Split(rows[1], "\t")
		if len(itemValue) > 2 {
			info.File = itemValue[0]
			info.Position = itemValue[1]
		}
	}

	return &info, nil
}

func (u *MysqlService) LoadDatabaseFile(req dto.OperationWithNameAndType) (string, error) {
	filePath := ""
	switch req.Type {
	case "mysql-conf":
		filePath = path.Join(global.CONF.System.DataDir, fmt.Sprintf("apps/mysql/%s/conf/my.cnf", req.Name))
	case "redis-conf":
		filePath = path.Join(global.CONF.System.DataDir, fmt.Sprintf("apps/redis/%s/conf/redis.conf", req.Name))
	case "slow-logs":
		filePath = path.Join(global.CONF.System.DataDir, fmt.Sprintf("apps/mysql/%s/data/1Panel-slow.log", req.Name))
	}
	if _, err := os.Stat(filePath); err != nil {
		return "", buserr.New("ErrHttpReqNotFound")
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func excuteSqlForMaps(containerName, password, command string) (map[string]string, error) {
	cmd := exec.Command("docker", "exec", containerName, "mysql", "-uroot", "-p"+password, "-e", command)
	stdout, err := cmd.CombinedOutput()
	stdStr := strings.ReplaceAll(string(stdout), "mysql: [Warning] Using a password on the command line interface can be insecure.\n", "")
	if err != nil || strings.HasPrefix(string(stdStr), "ERROR ") {
		return nil, errors.New(stdStr)
	}

	rows := strings.Split(stdStr, "\n")
	rowMap := make(map[string]string)
	for _, v := range rows {
		itemRow := strings.Split(v, "\t")
		if len(itemRow) == 2 {
			rowMap[itemRow[0]] = itemRow[1]
		}
	}
	return rowMap, nil
}

func excuteSqlForRows(containerName, password, command string) ([]string, error) {
	cmd := exec.Command("docker", "exec", containerName, "mysql", "-uroot", "-p"+password, "-e", command)
	stdout, err := cmd.CombinedOutput()
	stdStr := strings.ReplaceAll(string(stdout), "mysql: [Warning] Using a password on the command line interface can be insecure.\n", "")
	if err != nil || strings.HasPrefix(string(stdStr), "ERROR ") {
		return nil, errors.New(stdStr)
	}
	return strings.Split(stdStr, "\n"), nil
}

func updateMyCnf(oldFiles []string, group string, param string, value interface{}) []string {
	isOn := false
	hasGroup := false
	hasKey := false
	regItem, _ := regexp.Compile(`\[*\]`)
	var newFiles []string
	i := 0
	for _, line := range oldFiles {
		i++
		if strings.HasPrefix(line, group) {
			isOn = true
			hasGroup = true
			newFiles = append(newFiles, line)
			continue
		}
		if !isOn {
			newFiles = append(newFiles, line)
			continue
		}
		if strings.HasPrefix(line, param+"=") || strings.HasPrefix(line, "# "+param+"=") {
			newFiles = append(newFiles, fmt.Sprintf("%s=%v", param, value))
			hasKey = true
			continue
		}
		if regItem.Match([]byte(line)) || i == len(oldFiles) {
			isOn = false
			if !hasKey {
				newFiles = append(newFiles, fmt.Sprintf("%s=%v", param, value))
			}
			newFiles = append(newFiles, line)
			continue
		}
		newFiles = append(newFiles, line)
	}
	if !hasGroup {
		newFiles = append(newFiles, group+"\n")
		newFiles = append(newFiles, fmt.Sprintf("%s=%v\n", param, value))
	}
	return newFiles
}

func LoadMysqlClientByFrom(from string) (mysql.MysqlClient, string, error) {
	var (
		dbInfo  client.DBInfo
		version string
		err     error
	)

	dbInfo.From = from
	dbInfo.Timeout = 300
	if from != "local" {
		databaseItem, err := remoteDBRepo.Get(commonRepo.WithByName(from))
		if err != nil {
			return nil, "", err
		}
		dbInfo.Address = databaseItem.Address
		dbInfo.Port = databaseItem.Port
		dbInfo.Username = databaseItem.Username
		dbInfo.Password = databaseItem.Password
		version = databaseItem.Version

	} else {
		app, err := appInstallRepo.LoadBaseInfo("mysql", "")
		if err != nil {
			return nil, "", err
		}
		dbInfo.Address = app.ContainerName
		dbInfo.Username = "root"
		dbInfo.Password = app.Password
		version = app.Version
	}

	cli, err := mysql.NewMysqlClient(dbInfo)
	if err != nil {
		return nil, "", err
	}
	return cli, version, nil
}
