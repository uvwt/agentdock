//go:build windows

package securepath

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

type privateACE struct {
	typeID uint8
	flags  uint8
	mask   windows.ACCESS_MASK
	sid    string
}

// EnsurePrivate installs a protected DACL that grants full control only to the
// current user, SYSTEM and local administrators. Directory entries inherit the
// same boundary to newly-created children.
func EnsurePrivate(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("read current Windows user SID: %w", err)
	}
	inheritance := ""
	if info.IsDir() {
		inheritance = "OICI"
	}
	sddl := fmt.Sprintf(
		"D:P(A;%s;FA;;;%s)(A;%s;FA;;;SY)(A;%s;FA;;;BA)",
		inheritance,
		user.User.Sid.String(),
		inheritance,
		inheritance,
	)
	descriptor, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return fmt.Errorf("build private Windows DACL: %w", err)
	}
	dacl, _, err := descriptor.DACL()
	if err != nil {
		return fmt.Errorf("read private Windows DACL: %w", err)
	}
	// SetNamedSecurityInfo 会把目录上的可继承 ACE 传播到既有子项。AgentDockHome / DefaultDir
	// 可能包含数万文件；即使 DACL 完全没有变化，重复设置也会在 Windows 冷启动时产生几十秒 I/O。
	// 因此先按 ACE 语义比较当前 protected DACL，已经符合边界时不再触发写入和继承传播。
	if privateDACLMatches(path, dacl) {
		return nil
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		dacl,
		nil,
	); err != nil {
		return fmt.Errorf("secure private Windows path %s: %w", path, err)
	}
	return nil
}

func privateDACLMatches(path string, desired *windows.ACL) bool {
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil || descriptor == nil {
		// 读取失败时保留旧行为：继续尝试安装 private DACL，而不是因为优化路径降低安全边界。
		return false
	}
	control, _, err := descriptor.Control()
	if err != nil || control&windows.SE_DACL_PRESENT == 0 || control&windows.SE_DACL_PROTECTED == 0 {
		return false
	}
	current, _, err := descriptor.DACL()
	if err != nil {
		return false
	}
	return sameDACL(current, desired)
}

func sameDACL(current, desired *windows.ACL) bool {
	if current == nil || desired == nil {
		return current == desired
	}
	currentACEs, ok := privateACESet(current)
	if !ok {
		return false
	}
	desiredACEs, ok := privateACESet(desired)
	if !ok || len(currentACEs) != len(desiredACEs) {
		return false
	}
	for ace := range desiredACEs {
		if _, exists := currentACEs[ace]; !exists {
			return false
		}
	}
	return true
}

func privateACESet(acl *windows.ACL) (map[privateACE]struct{}, bool) {
	aces := make(map[privateACE]struct{}, int(acl.AceCount))
	for index := uint32(0); index < uint32(acl.AceCount); index++ {
		ace, ok := readPrivateACE(acl, index)
		if !ok {
			return nil, false
		}
		// Windows 在落盘 DACL 时可能合并完全相同的 ACE，例如服务以 SYSTEM
		// 身份运行时“当前用户”和显式 SYSTEM 是同一个 SID。重复 ACE 不改变权限语义。
		aces[ace] = struct{}{}
	}
	return aces, true
}

func readPrivateACE(acl *windows.ACL, index uint32) (privateACE, bool) {
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(acl, index, &ace); err != nil || ace == nil {
		return privateACE{}, false
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
		return privateACE{}, false
	}

	// ACCESS_ALLOWED_ACE 的 SID 从 SidStart（偏移 8）开始。先只读取固定 8 字节 SID
	// 头里的 SubAuthorityCount，再用 AceSize 约束完整 SID 长度；不能直接把 ACL 内部指针
	// 交给 Win32 SID API，否则损坏的 ACE 可能让 API 读过当前 ACE 边界。
	const sidHeaderSize = 8
	sidOffset := int(unsafe.Offsetof(ace.SidStart))
	aceSize := int(ace.Header.AceSize)
	if aceSize < sidOffset+sidHeaderSize {
		return privateACE{}, false
	}
	sidBytes := unsafe.Slice((*byte)(unsafe.Pointer(&ace.SidStart)), aceSize-sidOffset)
	sidLength := sidHeaderSize + int(sidBytes[1])*4
	if sidLength > len(sidBytes) {
		return privateACE{}, false
	}
	sidCopy := append([]byte(nil), sidBytes[:sidLength]...)
	sid := (*windows.SID)(unsafe.Pointer(&sidCopy[0]))
	if !sid.IsValid() || sid.Len() != sidLength {
		return privateACE{}, false
	}
	sidText := sid.String()
	if sidText == "" {
		return privateACE{}, false
	}
	return privateACE{
		typeID: ace.Header.AceType,
		flags:  ace.Header.AceFlags,
		mask:   ace.Mask,
		sid:    sidText,
	}, true
}
