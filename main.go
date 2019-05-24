package main // import "github.com/finkf/pcwusers"

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"regexp"

	"github.com/finkf/pcwgo/api"
	"github.com/finkf/pcwgo/db"
	"github.com/finkf/pcwgo/service"
	_ "github.com/go-sql-driver/mysql"
	log "github.com/sirupsen/logrus"
)

var (
	listen  string
	dsn     string
	rName   string
	rPass   string
	rEmail  string
	rInst   string
	debug   bool
	usersre = regexp.MustCompile(`/users/(\d+)`)
)

func init() {
	flag.StringVar(&listen, "listen", ":8080", "set listening host")
	flag.StringVar(&dsn, "dsn", "", "set mysql connection DSN (user:pass@proto(host)/dbname)")
	flag.StringVar(&rName, "root-name", "", "user name for the root account")
	flag.StringVar(&rEmail, "root-email", "", "email for the root account")
	flag.StringVar(&rPass, "root-password", "", "password for the root account")
	flag.StringVar(&rInst, "root-institute", "", "institute for the root account")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
}

func must(err error) {
	if err != nil {
		log.Fatalf("error: %v", err)
	}
}

func insertRoot() error {
	root := api.User{
		Name:      rName,
		Email:     rEmail,
		Institute: rInst,
		Admin:     true,
	}
	_, found, err := db.FindUserByEmail(service.Pool(), root.Email)
	if err != nil {
		return fmt.Errorf("cannot find user %s: %v", root, err)
	}
	if found { // root allready exists
		return nil
	}
	if err = db.InsertUser(service.Pool(), &root); err != nil {
		return fmt.Errorf("cannot create user %s: %v", root, err)
	}
	if err := db.SetUserPassword(service.Pool(), root, rPass); err != nil {
		return fmt.Errorf("cannot set password for %s: %v", root, err)
	}
	return nil
}

func main() {
	// flags
	flag.Parse()
	if debug {
		log.SetLevel(log.DebugLevel)
	}
	// database
	must(service.Init(dsn))
	defer service.Close()
	// root
	if rName != "" && rEmail != "" && rPass != "" {
		must(insertRoot())
	}
	// handlers
	http.HandleFunc("/users", service.WithLog(service.WithMethods(
		http.MethodPost, withPostUser(postUser()),
		http.MethodGet, getAllUsers())))
	http.HandleFunc("/users/", service.WithLog(service.WithMethods(
		http.MethodGet, withUserID(getUser()),
		http.MethodPut, withPostUser(withUserID(putUser())),
		http.MethodDelete, withUserID(deleteUser()))))
	log.Infof("listening on %s", listen)
	must(http.ListenAndServe(listen, nil))
}

func withUserID(f service.HandlerFunc) service.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, d *service.Data) {
		if err := service.ParseIDs(r.URL.String(), usersre, &d.ID); err != nil {
			service.ErrorResponse(w, http.StatusNotFound,
				"invalid user id: %v", err)
			return
		}
		log.Debugf("withUserID: %d", d.ID)
		f(w, r, d)
	}
}

func withPostUser(f service.HandlerFunc) service.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, d *service.Data) {
		var data api.CreateUserRequest
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			service.ErrorResponse(w, http.StatusBadRequest,
				"cannot read user: invalid data: %v", err)
			return
		}
		log.Debugf("withPostUser: %s", data.User)
		d.Post = data
		f(w, r, d)
	}
}

func getAllUsers() service.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, d *service.Data) {
		log.Debugf("get all users")
		users, err := db.FindAllUsers(service.Pool())
		if err != nil {
			service.ErrorResponse(w, http.StatusInternalServerError,
				"cannot list users: %v", err)
			return
		}
		service.JSONResponse(w, api.Users{Users: users})
	}
}

func getUser() service.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, d *service.Data) {
		u, found, err := db.FindUserByID(service.Pool(), int64(d.ID))
		if err != nil {
			service.ErrorResponse(w, http.StatusInternalServerError,
				"cannot get user: %v", err)
			return
		}
		if !found {
			service.ErrorResponse(w, http.StatusNotFound,
				"cannot get user: not found")
			return
		}
		log.Printf("get user: %s", u)
		service.JSONResponse(w, u)
	}
}

func postUser() service.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, d *service.Data) {
		t := db.NewTransaction(service.Pool().Begin())
		u := d.Post.(api.CreateUserRequest)
		t.Do(func(dtb db.DB) error {
			if err := db.InsertUser(dtb, &u.User); err != nil {
				return err
			}
			if err := db.SetUserPassword(dtb, u.User, u.Password); err != nil {
				return fmt.Errorf("cannot set password: %v", err)
			}
			return nil
		})
		if err := t.Done(); err != nil {
			service.ErrorResponse(w, http.StatusBadRequest, "cannot create user: %v", err)
			return
		}
		service.JSONResponse(w, u.User)
	}
}

func putUser() service.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, d *service.Data) {
		// this must not fail
		u := d.Post.(api.CreateUserRequest)
		t := db.NewTransaction(service.Pool().Begin())
		t.Do(func(dtb db.DB) error {
			if err := db.UpdateUser(dtb, u.User); err != nil {
				return err
			}
			if u.Password == "" { // do not update emtpy passwords
				return nil
			}
			if err := db.SetUserPassword(dtb, u.User, u.Password); err != nil {
				return fmt.Errorf("cannot set password: %v", err)
			}
			return nil
		})
		if err := t.Done(); err != nil {
			service.ErrorResponse(w, http.StatusInternalServerError,
				"cannot update user: %v", err)
			return
		}
		service.JSONResponse(w, u.User)
	}
}

func deleteUser() service.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, d *service.Data) {
		t := db.NewTransaction(service.Pool().Begin())
		t.Do(func(dtb db.DB) error {
			if err := reassignPackagesToOwner(d.ID); err != nil {
				return fmt.Errorf("cannot reassign pacakges: %v", err)
			}
			if err := deleteAllUserProjects(d.ID); err != nil {
				return fmt.Errorf("cannot delete projectes: %v", err)
			}
			// TODO: delete all projects of the particular user
			if err := db.DeleteUserByID(service.Pool(), int64(d.ID)); err != nil {
				return err
			}
			return nil
		})
		if err := t.Done(); err != nil {
			service.ErrorResponse(w, http.StatusInternalServerError,
				"cannot delete user: %v", err)
			return
		}
		w.WriteHeader(http.StatusOK)
	}
}

func reassignPackagesToOwner(id int) error {
	return errors.New("not implemented")
}

func deleteAllUserProjects(id int) error {
	return errors.New("not implemented")
}
