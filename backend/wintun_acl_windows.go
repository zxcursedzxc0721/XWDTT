//go:build windows

package backend

import "golang.org/x/sys/windows"

// restrictWintunACL оставляет доступ к wintun.dll только SYSTEM и группе
// Администраторов. DLL загружается elevated-процессом; без этого файл с
// правами 0644 между сверкой хеша и загрузкой мог подменить процесс того же
// пользователя → выполнение кода с правами администратора.
func restrictWintunACL(path string) error {
	sd, err := windows.SecurityDescriptorFromString("D:P(A;;FA;;;BA)(A;;FA;;;SY)")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
}
