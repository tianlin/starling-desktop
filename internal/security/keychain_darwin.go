//go:build darwin && cgo

package security

/*
#cgo LDFLAGS: -framework Security -framework CoreFoundation
#include <Security/Security.h>
#include <stdlib.h>
#include <string.h>

static CFMutableDictionaryRef starlingQuery(const char *account, SecKeychainRef chain) {
 CFMutableDictionaryRef q=CFDictionaryCreateMutable(NULL,0,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 CFStringRef a=CFStringCreateWithCString(NULL,account,kCFStringEncodingUTF8);
 CFDictionarySetValue(q,kSecClass,kSecClassGenericPassword);
 CFDictionarySetValue(q,kSecAttrService,CFSTR("io.github.tianlin.starling.session"));
 CFDictionarySetValue(q,kSecAttrAccount,a);
 CFDictionarySetValue(q,kSecAttrSynchronizable,kCFBooleanFalse);
 if(chain) {
  const void *values[]={chain};
  CFArrayRef list=CFArrayCreate(NULL,values,1,&kCFTypeArrayCallBacks);
  CFDictionarySetValue(q,kSecMatchSearchList,list); CFRelease(list);
 }
 CFRelease(a); return q;
}
static OSStatus starlingRead(const char *account, SecKeychainRef chain, void **bytes, long *size) {
 CFMutableDictionaryRef q=starlingQuery(account,chain);
 CFDictionarySetValue(q,kSecReturnData,kCFBooleanTrue);
 CFDictionarySetValue(q,kSecMatchLimit,kSecMatchLimitOne);
 CFTypeRef value=NULL; OSStatus status=SecItemCopyMatching(q,&value); CFRelease(q);
 if(status==errSecSuccess) {
  if(!value || CFGetTypeID(value)!=CFDataGetTypeID()) { if(value) CFRelease(value); return errSecDecode; }
  *size=CFDataGetLength((CFDataRef)value);
  if(*size>65536 || *size<=0) { CFRelease(value); return errSecDecode; }
  *bytes=malloc(*size);
  if(!*bytes) { CFRelease(value); return errSecAllocate; }
  memcpy(*bytes,CFDataGetBytePtr((CFDataRef)value),*size); CFRelease(value);
 }
 return status;
}
static OSStatus starlingWrite(const char *account, SecKeychainRef chain, const void *bytes, long size) {
 CFMutableDictionaryRef q=starlingQuery(account,chain);
 CFDataRef data=CFDataCreate(NULL,bytes,size);
 const void *keys[]={kSecValueData}; const void *values[]={data};
 CFDictionaryRef update=CFDictionaryCreate(NULL,keys,values,1,&kCFTypeDictionaryKeyCallBacks,&kCFTypeDictionaryValueCallBacks);
 OSStatus status=SecItemUpdate(q,update);
 if(status==errSecItemNotFound) {
  CFDictionaryRemoveValue(q,kSecMatchSearchList);
  if(chain) CFDictionarySetValue(q,kSecUseKeychain,chain);
  CFDictionarySetValue(q,kSecValueData,data);
  CFDictionarySetValue(q,kSecAttrLabel,CFSTR("Starling saved session"));
  status=SecItemAdd(q,NULL);
 }
 CFRelease(update); CFRelease(data); CFRelease(q); return status;
}
static OSStatus starlingDelete(const char *account,SecKeychainRef chain) {
 CFMutableDictionaryRef q=starlingQuery(account,chain); OSStatus status=SecItemDelete(q); CFRelease(q); return status;
}
static int starlingAvailable(SecKeychainRef chain) {
 if(chain) return 1;
 SecKeychainRef keychain=NULL; OSStatus status=SecKeychainCopyDefault(&keychain);
 if(keychain) CFRelease(keychain); return status==errSecSuccess;
}
static void starlingFree(void *data,long size) { if(data) { memset(data,0,size); free(data); } }
*/
import "C"
import (
	"starling/internal/model"
	"unsafe"
)

// chain is nil for the user's login keychain; native tests supply an isolated keychain.
type nativeKeychain struct{ chain C.SecKeychainRef }

// Used by native integration tests to avoid touching the login keychain.
func openIsolatedKeychain(path string) (nativeKeychain, func(), error) {
	p := C.CString(path)
	defer C.free(unsafe.Pointer(p))
	var chain C.SecKeychainRef
	if e := keychainError(C.SecKeychainOpen(p, &chain)); e != nil {
		return nativeKeychain{}, func() {}, e
	}
	return nativeKeychain{chain: chain}, func() { C.CFRelease(C.CFTypeRef(chain)) }, nil
}

func NewVault(path string) Vault         { return newKeychainVault(path, nativeKeychain{}) }
func (k nativeKeychain) Available() bool { return C.starlingAvailable(k.chain) != 0 }
func keychainError(status C.OSStatus) error {
	switch status {
	case C.errSecSuccess:
		return nil
	case C.errSecItemNotFound:
		return errKeychainMissing
	case C.errSecUserCanceled:
		return model.Err("SECURE_STORAGE", "钥匙串访问已取消，会话保存未完成。")
	case C.errSecAuthFailed:
		return model.Err("SECURE_STORAGE", "钥匙串访问被拒绝，请检查本应用的访问权限。")
	case C.errSecInteractionNotAllowed:
		return model.Err("SECURE_STORAGE", "钥匙串已锁定或当前无法交互，请解锁后重试。")
	case C.errSecDecode:
		return model.Err("SECURE_STORAGE", "钥匙串会话内容损坏，请重新登录。")
	default:
		return model.Err("SECURE_STORAGE", "系统钥匙串操作失败，请重试或使用仅本次会话。")
	}
}
func (k nativeKeychain) Read(account string) ([]byte, error) {
	a := C.CString(account)
	defer C.free(unsafe.Pointer(a))
	var data unsafe.Pointer
	var size C.long
	status := C.starlingRead(a, k.chain, &data, &size)
	defer C.starlingFree(data, size)
	if e := keychainError(status); e != nil {
		return nil, e
	}
	return C.GoBytes(data, C.int(size)), nil
}
func (k nativeKeychain) Write(account string, b []byte) error {
	a := C.CString(account)
	defer C.free(unsafe.Pointer(a))
	data := C.CBytes(b)
	defer C.starlingFree(data, C.long(len(b)))
	return keychainError(C.starlingWrite(a, k.chain, data, C.long(len(b))))
}
func (k nativeKeychain) Delete(account string) error {
	a := C.CString(account)
	defer C.free(unsafe.Pointer(a))
	return keychainError(C.starlingDelete(a, k.chain))
}
