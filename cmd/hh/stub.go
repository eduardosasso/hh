package main

import "errors"

var errTodo = errors.New("not implemented yet")

func lsCmd([]string) error     { return errTodo }
func openCmd([]string) error   { return errTodo }
func setupCmd([]string) error  { return errTodo }
func daemonCmd([]string) error { return errTodo }
func logsCmd([]string) error   { return errTodo }
func wrap([]string) error      { return errTodo }
