package dbfs

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql" // required to load into local namespace to
	// initialize sql driver mapping in sql.Open("mysql", ...)
	"github.com/CodeCollaborate/Server/modules/config"
	"github.com/CodeCollaborate/Server/utils"
)

type mysqlConn struct {
	config config.ConnCfg
	db     *sql.DB
}

func (di *DatabaseImpl) getMySQLConn() (*mysqlConn, error) {
	if di.mysqldb != nil && di.mysqldb.db != nil {
		err := di.mysqldb.db.Ping()
		if err == nil {
			return di.mysqldb, nil
		}
	}

	if di.mysqldb == nil || di.mysqldb.config == (config.ConnCfg{}) {
		di.mysqldb = new(mysqlConn)
		configMap := config.GetConfig()
		di.mysqldb.config = configMap.ConnectionConfig["MySQL"]
	}

	if di.mysqldb.config.Schema == "" {
		panic("No MySQL schema found in config")
	}

	connString := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?timeout=%ds&parseTime=true",
		di.mysqldb.config.Username,
		di.mysqldb.config.Password,
		di.mysqldb.config.Host,
		di.mysqldb.config.Port,
		di.mysqldb.config.Schema,
		di.mysqldb.config.Timeout)
	db, err := sql.Open("mysql", connString)
	if err == nil {
		for i := uint16(0); i < di.mysqldb.config.NumRetries; i++ {
			if err = db.Ping(); err != nil {
				err = ErrDbNotInitialized
				time.Sleep(3 * time.Second)
			} else {
				di.mysqldb.db = db
				err = nil
				break
			}
		}
	}

	utils.LogError("Unable to connect to MySQL", err, utils.LogFields{
		"Host":   di.mysqldb.config.Host,
		"Port":   di.mysqldb.config.Port,
		"Schema": di.mysqldb.config.Schema,
	})
	if err != nil {
		di.mysqldb = nil
	}
	return di.mysqldb, err
}

// CloseMySQL closes the MySQL db connection
// YOU PROBABLY DON'T NEED TO RUN THIS EVER
func (di *DatabaseImpl) CloseMySQL() error {
	if di.mysqldb != nil && di.mysqldb.db != nil {
		err := di.mysqldb.db.Close()
		di.mysqldb = nil
		return err
	}
	return ErrDbNotInitialized
}

/**
STORED PROCEDURES
*/

// MySQLUserRegister registers a new user in MySQL
func (di *DatabaseImpl) MySQLUserRegister(user UserMeta) error {
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return err
	}

	result, err := mysqlConn.db.Exec("CALL user_register(?,?,?,?,?)", user.Username, user.Password, user.Email, user.FirstName, user.LastName)
	if err != nil {
		return err
	}
	numRows, err := result.RowsAffected()

	if err != nil || numRows == 0 {
		return ErrNoDbChange
	}

	return nil
}

// MySQLUserGetPass is used to get the key and hash of a stored password to verify that a value is correct
func (di *DatabaseImpl) MySQLUserGetPass(username string) (password string, err error) {
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return "", err
	}

	rows, err := mysqlConn.db.Query("CALL user_get_password(?)", username)
	if err != nil {
		return "", err
	}

	password = ""

	for rows.Next() {
		err = rows.Scan(&password)
		if err != nil {
			return "", err
		}
	}

	return password, nil
}

// MySQLUserDelete deletes a user from MySQL
func (di *DatabaseImpl) MySQLUserDelete(username string) ([]int64, error) {
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return []int64{}, err
	}

	rows, err := mysqlConn.db.Query("Call user_get_projectids(?)", username)

	var projectIDs []int64
	for rows.Next() {
		projectID := int64(-1)
		err = rows.Scan(&projectID)
		if err != nil {
			return []int64{}, err
		}
		if projectID == -1 {
			return []int64{}, ErrNoData
		}
		projectIDs = append(projectIDs, projectID)
	}

	result, err := mysqlConn.db.Exec("CALL user_delete(?)", username)
	if err != nil {
		return []int64{}, err
	}
	numrows, err := result.RowsAffected()

	if err != nil || numrows == 0 {
		return []int64{}, ErrNoDbChange
	}

	return projectIDs, nil
}

// MySQLUserLookup returns user information about a user with the username 'username'
func (di *DatabaseImpl) MySQLUserLookup(username string) (user UserMeta, err error) {
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return user, err
	}

	rows, err := mysqlConn.db.Query("CALL user_lookup(?)", username)
	if err != nil {
		return user, err
	}

	result := false
	for rows.Next() {
		err = rows.Scan(&user.FirstName, &user.LastName, &user.Email, &user.Username)
		if err != nil {
			return user, err
		}
		result = true
	}
	if !result {
		return user, ErrNoData
	}
	return user, nil
}

// MySQLUserProjects returns the projectID, the project name, and the permission level the user `username` has on that project
func (di *DatabaseImpl) MySQLUserProjects(username string) ([]ProjectMeta, error) {
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return nil, err
	}

	rows, err := mysqlConn.db.Query("CALL user_projects(?)", username)
	if err != nil {
		return nil, err
	}

	projects := []ProjectMeta{}

	for rows.Next() {
		project := ProjectMeta{}
		err = rows.Scan(&project.ProjectID, &project.Name, &project.PermissionLevel)
		if err != nil {
			return nil, err
		}
		projects = append(projects, project)
	}

	return projects, nil
}

// MySQLProjectCreate create a new project in MySQL
func (di *DatabaseImpl) MySQLProjectCreate(username string, projectName string) (projectID int64, err error) {
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return -1, err
	}

	rows, err := mysqlConn.db.Query("CALL project_create(?,?)", projectName, username)
	if err != nil {
		return -1, err
	}
	for rows.Next() {
		err = rows.Scan(&projectID)
		if err != nil {
			return -1, err
		}
	}

	return projectID, nil
}

// MySQLProjectDelete deletes a project from MySQL
func (di *DatabaseImpl) MySQLProjectDelete(projectID int64, senderID string) error {
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return err
	}

	result, err := mysqlConn.db.Exec("CALL project_delete(?,?)", projectID, senderID)
	if err != nil {
		return err
	}
	numrows, err := result.RowsAffected()

	if err != nil || numrows == 0 {
		return ErrNoDbChange
	}
	return nil
}

// MySQLProjectGetFiles returns the Files from the project with projectID = projectID
func (di *DatabaseImpl) MySQLProjectGetFiles(projectID int64) (files []FileMeta, err error) {
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return nil, err
	}

	rows, err := mysqlConn.db.Query("CALL project_get_files(?)", projectID)
	if err != nil {
		return nil, err
	}

	files = []FileMeta{}

	for rows.Next() {
		file := FileMeta{}
		err = rows.Scan(&file.FileID, &file.Creator, &file.CreationDate, &file.RelativePath, &file.ProjectID, &file.Filename)
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}

	return files, nil
}

// MySQLProjectGrantPermission gives the user `grantUsername` the permission `permissionLevel` on project `projectID`
func (di *DatabaseImpl) MySQLProjectGrantPermission(projectID int64, grantUsername string, permissionLevel int8, grantedByUsername string) error {
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return err
	}

	result, err := mysqlConn.db.Exec("CALL project_grant_permissions(?, ?, ?, ?)", projectID, grantUsername, permissionLevel, grantedByUsername)
	if err != nil {
		return err
	}
	numrows, err := result.RowsAffected()

	if err != nil || numrows == 0 {
		return ErrNoDbChange
	}
	return nil
}

// MySQLProjectRevokePermission removes revokeUsername's permissions from the project
// DOES NOT WORK FOR OWNER (which is kinda a good thing)
func (di *DatabaseImpl) MySQLProjectRevokePermission(projectID int64, revokeUsername string, revokedByUsername string) error {
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return err
	}

	result, err := mysqlConn.db.Exec("CALL project_revoke_permissions(?, ?)", projectID, revokeUsername)
	if err != nil {
		return err
	}
	numrows, err := result.RowsAffected()

	if err != nil || numrows == 0 {
		return ErrNoDbChange
	}
	return nil
}

// MySQLUserProjectPermissionLookup returns the permission level of `username` on the project with the given projectID
func (di *DatabaseImpl) MySQLUserProjectPermissionLookup(projectID int64, username string) (int8, error) {
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return 0, err
	}

	rows, err := mysqlConn.db.Query("CALL user_project_permission(?, ?)", username, projectID)
	if err != nil {
		return 0, err
	}
	var permission int8

	result := false
	for rows.Next() {
		err = rows.Scan(&permission)
		if err != nil {
			return 0, err
		}
		result = true
	}
	if !result {
		return 0, ErrNoData
	}

	return permission, nil
}

// MySQLProjectRename allows for you to rename projects
func (di *DatabaseImpl) MySQLProjectRename(projectID int64, newName string) error {
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return err
	}

	result, err := mysqlConn.db.Exec("CALL project_rename(?, ?)", projectID, newName)
	if err != nil {
		return err
	}
	numrows, err := result.RowsAffected()

	if err != nil || numrows == 0 {
		return ErrNoDbChange
	}
	return nil
}

// MySQLProjectLookup returns the project name and permissions for a project with ProjectID = 'projectID'
//
// Looking them up 1 at a time may seem worse, however we're looking up rows based on their primary key
// so we get the speed benefits of it having a unique index on it
// Thoughts:
// 		FIND_IN_SET doesn't use any indices at all,
// 		both IN and FIND_IN_SET have issues with integers
// 		more issues when there are a variable number of ID's because MySQL doesn't have arrays
//
// http://stackoverflow.com/a/8150183 <- preferred if we switch b/c FIND_IN_SET doesn't use indexes
func (di *DatabaseImpl) MySQLProjectLookup(projectID int64, username string) (name string, permissions map[string]ProjectPermission, err error) {
	permissions = make(map[string](ProjectPermission))
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return "", permissions, err
	}

	// TODO (optional): un-hardcode '10' as the owner constant in the MySQL ProjectLookup stored proc

	rows, err := mysqlConn.db.Query("CALL project_lookup(?)", projectID)
	if err != nil {
		return "", permissions, err
	}

	result := false
	var hasAccess = false
	for rows.Next() {
		perm := ProjectPermission{}
		var timeVal string
		err = rows.Scan(&name, &perm.Username, &perm.PermissionLevel, &perm.GrantedBy, &timeVal)
		perm.GrantedDate, _ = time.Parse("2006-01-02 15:04:05", timeVal)
		if err != nil {
			return "", permissions, err
		}
		if !hasAccess && perm.PermissionLevel > 0 && perm.Username == username {
			hasAccess = true
		}
		permissions[perm.Username] = perm
		result = true
	}

	// verify user has access to view this info
	if !result || !hasAccess {
		return "", make(map[string](ProjectPermission)), ErrNoData
	}
	return name, permissions, err
}

// MySQLFileCreate create a new file in MySQL
func (di *DatabaseImpl) MySQLFileCreate(username string, filename string, relativePath string, projectID int64) (int64, error) {
	filename = filepath.Clean(filename)
	if strings.Contains(filename, filePathSeparator) || strings.Contains(filename, "..") {
		return -1, ErrMaliciousRequest
	}

	relativePath = filepath.Clean(relativePath)
	if strings.HasPrefix(relativePath, "..") {
		return -1, ErrMaliciousRequest
	}

	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return -1, err
	}

	rows, err := mysqlConn.db.Query("CALL file_create(?,?,?,?)", username, filename, relativePath, projectID)
	if err != nil {
		return -1, err
	}

	var fileID int64
	for rows.Next() {
		err = rows.Scan(&fileID)
		if err != nil {
			return -1, ErrNoDbChange
		}
	}

	return fileID, nil
}

// MySQLFileDelete deletes a file from the MySQL database
// this does not delete the actual file
func (di *DatabaseImpl) MySQLFileDelete(fileID int64) error {
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return err
	}

	result, err := mysqlConn.db.Exec("CALL file_delete(?)", fileID)
	if err != nil {
		return err
	}
	numrows, err := result.RowsAffected()

	if err != nil || numrows == 0 {
		return ErrNoDbChange
	}
	return nil
}

// MySQLFileMove updates MySQL with the  new path of the file with FileID == 'fileID'
func (di *DatabaseImpl) MySQLFileMove(fileID int64, newPath string) error {
	newPathClean := filepath.Clean(newPath)
	if strings.HasPrefix(newPathClean, "..") {
		return ErrMaliciousRequest
	}

	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return err
	}

	result, err := mysqlConn.db.Exec("CALL file_move(?, ?)", fileID, newPathClean)
	if err != nil {
		return err
	}
	numrows, err := result.RowsAffected()

	if err != nil || numrows == 0 {
		return ErrNoDbChange
	}
	return nil
}

// MySQLFileRename updates MySQL with the new name of the file with FileID == 'fileID'
func (di *DatabaseImpl) MySQLFileRename(fileID int64, newName string) error {
	if strings.Contains(newName, filePathSeparator) {
		return ErrMaliciousRequest
	}

	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return err
	}

	result, err := mysqlConn.db.Exec("CALL file_rename(?, ?)", fileID, newName)
	if err != nil {
		return err
	}
	numrows, err := result.RowsAffected()

	if err != nil || numrows == 0 {
		return ErrNoDbChange
	}
	return nil
}

// MySQLFileGetInfo returns the meta data about the given file
func (di *DatabaseImpl) MySQLFileGetInfo(fileID int64) (FileMeta, error) {
	file := FileMeta{}
	mysqlConn, err := di.getMySQLConn()
	if err != nil {
		return file, err
	}

	rows, err := mysqlConn.db.Query("CALL file_get_info(?)", fileID)
	if err != nil {
		return file, err
	}

	file.FileID = fileID
	for rows.Next() {
		err = rows.Scan(&file.Creator, &file.CreationDate, &file.RelativePath, &file.ProjectID, &file.Filename)
		if err != nil {
			return file, err
		}
	}

	return file, nil
}
