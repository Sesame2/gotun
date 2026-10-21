//go:build darwin && cgo

#import <Cocoa/Cocoa.h>
extern void gotunTraySelected(char *action);

@interface GoTunTrayTarget : NSObject
- (void)selected:(NSMenuItem *)sender;
@end
@implementation GoTunTrayTarget
- (void)selected:(NSMenuItem *)sender {
    gotunTraySelected((char *)[(NSString *)sender.representedObject UTF8String]);
}
@end

static NSStatusItem *goTunStatusItem;
static GoTunTrayTarget *goTunTrayTarget;

static NSMenu *goTunMenu(NSArray *items) {
    NSMenu *menu = [[[NSMenu alloc] init] autorelease];
    menu.autoenablesItems = NO;
    for (NSDictionary *item in items) {
        if ([item[@"separator"] boolValue]) {
            [menu addItem:[NSMenuItem separatorItem]];
            continue;
        }
        NSMenuItem *entry = [[[NSMenuItem alloc] initWithTitle:item[@"label"] action:@selector(selected:) keyEquivalent:@""] autorelease];
        entry.target = goTunTrayTarget;
        entry.representedObject = item[@"id"];
        entry.enabled = ![item[@"disabled"] boolValue];
        entry.state = [item[@"checked"] boolValue] ? NSControlStateValueOn : NSControlStateValueOff;
        if ([item[@"children"] count] > 0) entry.submenu = goTunMenu(item[@"children"]);
        [menu addItem:entry];
    }
    return menu;
}

int gotun_tray_start(const void *icon, int length) {
 @autoreleasepool {
    NSData *data = [NSData dataWithBytes:icon length:length];
    __block BOOL ready = NO;
    void (^create)(void) = ^{
        if (!goTunStatusItem) {
            goTunTrayTarget = [[GoTunTrayTarget alloc] init];
            goTunStatusItem = [[[NSStatusBar systemStatusBar] statusItemWithLength:NSSquareStatusItemLength] retain];
            NSImage *image = [[[NSImage alloc] initWithData:data] autorelease];
            image.size = NSMakeSize(18, 18);
            goTunStatusItem.button.image = image;
            goTunStatusItem.button.toolTip = @"GoTun";
            [goTunStatusItem.button setAccessibilityLabel:@"GoTun"];
        }
        ready = goTunStatusItem != nil;
    };
    if ([NSThread isMainThread]) create(); else dispatch_sync(dispatch_get_main_queue(), create);
    return ready;
 }
}

void gotun_tray_update(const char *json) {
 @autoreleasepool {
    NSData *data = [[NSString stringWithUTF8String:json] dataUsingEncoding:NSUTF8StringEncoding];
    NSArray *items = [NSJSONSerialization JSONObjectWithData:data options:0 error:NULL];
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            if (goTunStatusItem) goTunStatusItem.menu = goTunMenu(items);
        }
    });
 }
}

void gotun_tray_close(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!goTunStatusItem) return;
        [[NSStatusBar systemStatusBar] removeStatusItem:goTunStatusItem];
        [goTunStatusItem release]; goTunStatusItem = nil;
        [goTunTrayTarget release]; goTunTrayTarget = nil;
    });
}
