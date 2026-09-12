//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
extern void starlingEvent(int event);

@interface StarlingIntegration : NSObject
@property(nonatomic, retain) NSStatusItem *item;
@end
@implementation StarlingIntegration
- (void)show:(id)sender { starlingEvent(1); }
- (void)toggle:(id)sender { starlingEvent(2); }
- (void)quit:(id)sender { starlingEvent(3); }
- (void)pause:(id)sender { starlingEvent(4); }
@end
static StarlingIntegration *integration;

static void onMain(void (^block)(void)) {
 if([NSThread isMainThread]) block(); else dispatch_sync(dispatch_get_main_queue(),block);
}
void starlingStart(const char *name) {
 NSString *notification=[NSString stringWithUTF8String:name];
 onMain(^{
  integration=[[StarlingIntegration alloc] init];
  integration.item=[[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
  integration.item.button.title=@"星听";
  integration.item.button.toolTip=@"Starling";
  NSMenu *menu=[[NSMenu alloc] init];
  NSArray *titles=@[@"显示窗口",@"播放 / 暂停",@"退出 Starling"];
  SEL actions[]={@selector(show:),@selector(toggle:),@selector(quit:)};
  for(int i=0;i<3;i++) {
   NSMenuItem *item=[[NSMenuItem alloc] initWithTitle:titles[i] action:actions[i] keyEquivalent:@""];
   item.target=integration; [menu addItem:item]; [item release];
  }
  integration.item.menu=menu; [menu release];
  [[NSNotificationCenter defaultCenter] addObserver:integration selector:@selector(show:) name:NSApplicationDidBecomeActiveNotification object:NSApp];
  [[[NSWorkspace sharedWorkspace] notificationCenter] addObserver:integration selector:@selector(pause:) name:NSWorkspaceWillSleepNotification object:nil];
  [[[NSWorkspace sharedWorkspace] notificationCenter] addObserver:integration selector:@selector(pause:) name:NSWorkspaceDidWakeNotification object:nil];
  if(notification.length) [[NSDistributedNotificationCenter defaultCenter] addObserver:integration selector:@selector(show:) name:notification object:nil suspensionBehavior:NSNotificationSuspensionBehaviorDeliverImmediately];
 });
}
void starlingStop(void) {
 onMain(^{
  if(!integration) return;
  [[NSNotificationCenter defaultCenter] removeObserver:integration];
  [[[NSWorkspace sharedWorkspace] notificationCenter] removeObserver:integration];
  [[NSDistributedNotificationCenter defaultCenter] removeObserver:integration];
  [[NSStatusBar systemStatusBar] removeStatusItem:integration.item];
  integration.item=nil; [integration release]; integration=nil;
 });
}
void starlingNotify(const char *name) {
 @autoreleasepool {
  [[NSDistributedNotificationCenter defaultCenter] postNotificationName:[NSString stringWithUTF8String:name] object:nil userInfo:nil deliverImmediately:YES];
 }
}
int starlingOpen(const char *url) {
 __block BOOL opened=NO;
 NSString *value=[NSString stringWithUTF8String:url];
 onMain(^{ opened=[[NSWorkspace sharedWorkspace] openURL:[NSURL URLWithString:value]]; });
 return opened;
}
void starlingAlert(const char *text) {
 @autoreleasepool {
  NSAlert *alert=[[NSAlert alloc] init];
  alert.messageText=@"Starling"; alert.informativeText=[NSString stringWithUTF8String:text];
  [alert runModal]; [alert release];
 }
}
