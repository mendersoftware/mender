// Copyright 2016 Mender Software AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

package parser

import (
	"archive/tar"
	"errors"
	"io"
	"time"

	"github.com/mendersoftware/artifacts/metadata"
)

type UpdateFile struct {
	Name      string
	Path      string
	Size      int64
	Date      time.Time
	Checksum  []byte
	Signature []byte
}

type Reader interface {
	ParseHeader(tr *tar.Reader, hdr *tar.Header, hPath string) error
	ParseData(r io.Reader) error

	GetUpdateType() *metadata.UpdateType
	GetUpdateFiles() map[string]UpdateFile
	GetDeviceType() string
	GetMetadata() *metadata.AllMetadata
}

type Writer interface {
	ArchiveHeader(tw *tar.Writer, srcDir, dstDir string, updFiles []string) error
	ArchiveData(tw *tar.Writer, srcDir, dst string) error
	Copy() Parser
}

type Parser interface {
	Reader
	Writer
}

type ParseManager struct {
	// generic parser for basic archive reading and validataing
	gParser Parser
	// list of registered parsers for specific types
	pFactory map[string]Parser
	// parser instances produced by factory to parse specific update type
	pWorker Workers
}

type Workers map[string]Parser

func NewParseManager() *ParseManager {
	return &ParseManager{
		nil,
		make(map[string]Parser, 0),
		make(Workers, 0),
	}
}

func (p *ParseManager) GetWorkers() Workers {
	return p.pWorker
}

func (p *ParseManager) PushWorker(parser Parser, update string) error {
	if _, ok := p.pWorker[update]; ok {
		return errors.New("parser: already registered")
	}
	p.pWorker[update] = parser
	return nil
}

func (p *ParseManager) GetWorker(update string) (Parser, error) {
	if p, ok := p.pWorker[update]; ok {
		return p, nil
	}
	return nil, errors.New("parser: can not find worker for update " + update)
}

func (p *ParseManager) Register(parser Parser) error {
	parsingType := parser.GetUpdateType().Type
	if _, ok := p.pFactory[parsingType]; ok {
		return errors.New("parser: already registered")
	}
	p.pFactory[parsingType] = parser
	return nil
}

func (p *ParseManager) GetRegistered(parsingType string) (Parser, error) {
	parser, ok := p.pFactory[parsingType]
	if !ok {
		return nil, errors.New("parser: does not exist")
	}
	return parser.Copy(), nil
}

func (p *ParseManager) SetGeneric(parser Parser) {
	p.gParser = parser
}

func (p *ParseManager) GetGeneric() Parser {
	return p.gParser
}
