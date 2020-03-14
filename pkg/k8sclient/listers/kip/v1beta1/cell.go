/*
Copyright 2019 Elotl Inc.
*/

// Code generated by lister-gen. DO NOT EDIT.

package v1beta1

import (
	v1beta1 "github.com/elotl/kip/pkg/apis/kip/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// CellLister helps list Cells.
type CellLister interface {
	// List lists all Cells in the indexer.
	List(selector labels.Selector) (ret []*v1beta1.Cell, err error)
	// Get retrieves the Cell from the index for a given name.
	Get(name string) (*v1beta1.Cell, error)
	CellListerExpansion
}

// cellLister implements the CellLister interface.
type cellLister struct {
	indexer cache.Indexer
}

// NewCellLister returns a new CellLister.
func NewCellLister(indexer cache.Indexer) CellLister {
	return &cellLister{indexer: indexer}
}

// List lists all Cells in the indexer.
func (s *cellLister) List(selector labels.Selector) (ret []*v1beta1.Cell, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1beta1.Cell))
	})
	return ret, err
}

// Get retrieves the Cell from the index for a given name.
func (s *cellLister) Get(name string) (*v1beta1.Cell, error) {
	obj, exists, err := s.indexer.GetByKey(name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1beta1.Resource("cell"), name)
	}
	return obj.(*v1beta1.Cell), nil
}
