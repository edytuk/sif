// Copyright (c) 2018, Sylabs Inc. All rights reserved.
// Copyright (c) 2017, SingularityWare, LLC. All rights reserved.
// Copyright (c) 2017, Yannick Cote <yhcote@gmail.com> All rights reserved.
// This software is licensed under a 3-clause BSD license. Please consult the
// LICENSE file distributed with the sources of this project regarding your
// rights to use or distribute this software.

package sif

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"syscall"
)

// Read the global header from the container file
func readHeader(fimg *FileImage) error {
	if err := binary.Read(fimg.Reader, binary.LittleEndian, &fimg.Header); err != nil {
		return fmt.Errorf("reading global header from container file: %s", err)
	}

	return nil
}

// Read the used descriptors and populate an in-memory representation of those in node list
func readDescriptors(fimg *FileImage) error {
	// start by positioning us to the start of descriptors
	_, err := fimg.Reader.Seek(fimg.Header.Descroff, 0)
	if err != nil {
		return fmt.Errorf("seek() setting to descriptors start: %s", err)
	}

	// Initialize descriptor array (slice) and read them all from file
	fimg.DescrArr = make([]Descriptor, fimg.Header.Dtotal)
	if err := binary.Read(fimg.Reader, binary.LittleEndian, &fimg.DescrArr); err != nil {
		fimg.DescrArr = nil
		return fmt.Errorf("reading descriptor array from container file: %s", err)
	}

	return nil
}

// Look at key fields from the global header to assess SIF validity.
// `runnable' checks is current container can run on host.
func isValidSif(fimg *FileImage, runnable bool) error {
	var arch string

	// determine HdrArch value based on GOARCH
	if arch = GetSIFArch(runtime.GOARCH); arch == HdrArchUnknown {
		return fmt.Errorf("GOARCH %v not supported", runtime.GOARCH)
	}

	// check various header fields
	if string(fimg.Header.Magic[:HdrMagicLen-1]) != HdrMagic {
		return fmt.Errorf("invalid SIF file: Magic |%s| want |%s|", fimg.Header.Magic, HdrMagic)
	}
	if string(fimg.Header.Version[:HdrVersionLen-1]) != HdrVersion {
		return fmt.Errorf("invalid SIF file: Version %s want %s", fimg.Header.Version, HdrVersion)
	}
	if runnable {
		// Assume amd64 runs i386 code
		if (string(fimg.Header.Arch[:HdrArchLen-1]) == HdrArchAMD64) && (arch != HdrArch386 && arch != HdrArchAMD64) {
			return fmt.Errorf("invalid SIF file: Arch %s want %s", fimg.Header.Arch, arch)
		}
		if string(fimg.Header.Arch[:HdrArchLen-1]) != arch {
			return fmt.Errorf("invalid SIF file: Arch %s want %s", fimg.Header.Arch, arch)
		}
	}

	return nil
}

// mapFile takes a file pointer and returns a slice of bytes representing the file data
func (fimg *FileImage) mapFile(rdonly bool) error {
	prot := syscall.PROT_READ
	flags := syscall.MAP_PRIVATE

	info, err := fimg.Fp.Stat()
	if err != nil {
		return fmt.Errorf("while trying to size SIF file to mmap")
	}
	fimg.Filesize = info.Size()

	size := nextAligned(info.Size(), syscall.Getpagesize())
	if int64(int(size)) < info.Size() {
		return fmt.Errorf("file is to big to be mapped")
	}

	if rdonly == false {
		prot = syscall.PROT_WRITE
		flags = syscall.MAP_SHARED
	}

	fimg.Filedata, err = syscall.Mmap(int(fimg.Fp.Fd()), 0, int(size), prot, flags)
	if err != nil {
		return fmt.Errorf("while trying to call mmap on SIF file")
	}

	// create and associate a new bytes.Reader on top of mmap'ed data from file
	fimg.Reader = bytes.NewReader(fimg.Filedata)

	return nil
}

func (fimg *FileImage) unmapFile() error {
	if err := syscall.Munmap(fimg.Filedata); err != nil {
		return fmt.Errorf("while calling unmapping SIF file")
	}
	return nil
}

// LoadContainer is responsible for loading a SIF container file. It takes
// the container file name, and whether the file is opened as read-only
// as arguments.
func LoadContainer(filename string, rdonly bool) (fimg FileImage, err error) {
	if rdonly { // open SIF rdonly if mounting immutable partitions or inspecting the image
		if fimg.Fp, err = os.Open(filename); err != nil {
			return fimg, fmt.Errorf("opening(RDONLY) container file: %s", err)
		}
	} else { // open SIF read-write when adding and removing data objects
		if fimg.Fp, err = os.OpenFile(filename, os.O_RDWR, 0644); err != nil {
			return fimg, fmt.Errorf("opening(RDWR) container file: %s", err)
		}
	}

	// get a memory map of the SIF file
	if err = fimg.mapFile(rdonly); err != nil {
		return
	}

	// read global header from SIF file
	if err = readHeader(&fimg); err != nil {
		return
	}

	// validate global header
	if err = isValidSif(&fimg, true); err != nil {
		return
	}

	// read descriptor array from SIF file
	if err = readDescriptors(&fimg); err != nil {
		return
	}

	return
}

// LoadContainerFp is responsible for loading a SIF container file. It takes
// a *os.File pointing to an opened file, and whether the file is opened as
// read-only for arguments.
func LoadContainerFp(fp *os.File, rdonly bool) (fimg FileImage, err error) {
	if fp == nil {
		return fimg, fmt.Errorf("provided fp for file is invalid")
	}

	fimg.Fp = fp

	// get a memory map of the SIF file
	if err = fimg.mapFile(rdonly); err != nil {
		return
	}

	// read global header from SIF file
	if err = readHeader(&fimg); err != nil {
		return
	}

	// validate global header
	if err = isValidSif(&fimg, true); err != nil {
		return
	}

	// read descriptor array from SIF file
	if err = readDescriptors(&fimg); err != nil {
		return
	}

	return fimg, nil
}

// LoadContainerReader is responsible for processing SIF data from a byte stream
// and extract various components like the global header, descriptors and even
// perhaps data, depending on how much is read from the source.
func LoadContainerReader(b *bytes.Reader) (fimg FileImage, err error) {
	fimg.Reader = b

	// read global header from SIF file
	if err = readHeader(&fimg); err != nil {
		return
	}

	// validate global header
	if err = isValidSif(&fimg, false); err != nil {
		return
	}

	// in the case where the reader buffer doesn't include descriptor data, we
	// don't return an error and DescrArr will be set to nil
	readDescriptors(&fimg)

	return fimg, nil
}

// UnloadContainer closes the SIF container file and free associated resources if needed
func (fimg *FileImage) UnloadContainer() (err error) {
	// if SIF data comes from file, not a slice buffer (see LoadContainer() variants)
	if fimg.Fp != nil {
		if err = fimg.unmapFile(); err != nil {
			return
		}
		if err = fimg.Fp.Close(); err != nil {
			return fmt.Errorf("closing SIF file failed, corrupted: don't use: %s", err)
		}
	}
	return
}
