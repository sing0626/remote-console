"""Generate the SwiftUI/WebRTC Xcode project. This step also runs on Windows."""
from pathlib import Path
import plistlib,json

root=Path(__file__).resolve().parent
info=plistlib.loads((root/'Info.plist').read_bytes())
release=json.loads((root.parents[1]/'release.json').read_text(encoding='utf-8'))
brand=json.loads((root.parents[1]/'branding/brand.json').read_text(encoding='utf-8'))
controller_bundle_id=brand['application_ids']['ios_controller']
info['CFBundleShortVersionString']=release['version']
info['CFBundleVersion']=str(release['build'])
info['CFBundleDisplayName']=brand['name']
(root/'Info.plist').write_bytes(plistlib.dumps(info,sort_keys=False))
project=root/'RemoteController.xcodeproj';project.mkdir(exist_ok=True)
ids={name:f'{index:024X}' for index,name in enumerate(['project','root','products','target','file','info','product','buildfile','sources','frameworks','resources','projectconfigs','targetconfigs','pd','pr','td','tr','endpoint','endpointbuild','native','nativebuild','protocol','protocolbuild','webrtcpkg','webrtcproduct','webrtcbuild','design','designbuild','uitarget','uitfile','uitproduct','uitbuildfile','uitsources','uitframeworks','uitresources','uitconfigs','uitdebug','uitrelease','uitdependency','uitproxy','trust','trustbuild','audioh','audiom','audiobuild','bridge','assets','assetsbuild'],1)}
def i(name):return ids[name]
objects=[
f'{i("assets")} = {{isa=PBXFileReference;lastKnownFileType=folder.assetcatalog;path=Assets.xcassets;sourceTree="<group>";}};',
f'{i("assetsbuild")} = {{isa=PBXBuildFile;fileRef={i("assets")};}};',
f'{i("audioh")} = {{isa=PBXFileReference;lastKnownFileType=sourcecode.c.h;path=RCPlaybackAudioDevice.h;sourceTree="<group>";}};',
f'{i("audiom")} = {{isa=PBXFileReference;lastKnownFileType=sourcecode.c.objc;path=RCPlaybackAudioDevice.m;sourceTree="<group>";}};',
f'{i("audiobuild")} = {{isa=PBXBuildFile;fileRef={i("audiom")};}};',
f'{i("bridge")} = {{isa=PBXFileReference;lastKnownFileType=sourcecode.c.h;path=RemoteController-Bridging-Header.h;sourceTree="<group>";}};',

f'{i("trust")} = {{isa=PBXFileReference;lastKnownFileType=sourcecode.swift;path=TrustedController.swift;sourceTree="<group>";}};',
f'{i("trustbuild")} = {{isa=PBXBuildFile;fileRef={i("trust")};}};',

f'{i("uitfile")} = {{isa=PBXFileReference;lastKnownFileType=sourcecode.swift;path=MotionUITests.swift;sourceTree="<group>";}};',
f'{i("uitproduct")} = {{isa=PBXFileReference;explicitFileType=wrapper.cfbundle;path=RemoteControllerUITests.xctest;sourceTree=BUILT_PRODUCTS_DIR;}};',
f'{i("uitbuildfile")} = {{isa=PBXBuildFile;fileRef={i("uitfile")};}};',
f'{i("uitsources")} = {{isa=PBXSourcesBuildPhase;buildActionMask=2147483647;files=({i("uitbuildfile")},);runOnlyForDeploymentPostprocessing=0;}};',
f'{i("uitframeworks")} = {{isa=PBXFrameworksBuildPhase;buildActionMask=2147483647;files=();runOnlyForDeploymentPostprocessing=0;}};',
f'{i("uitresources")} = {{isa=PBXResourcesBuildPhase;buildActionMask=2147483647;files=();runOnlyForDeploymentPostprocessing=0;}};',
f'{i("uitproxy")} = {{isa=PBXContainerItemProxy;containerPortal={i("project")};proxyType=1;remoteGlobalIDString={i("target")};remoteInfo=RemoteController;}};',
f'{i("uitdependency")} = {{isa=PBXTargetDependency;target={i("target")};targetProxy={i("uitproxy")};}};',
f'{i("uitarget")} = {{isa=PBXNativeTarget;buildConfigurationList={i("uitconfigs")};buildPhases=({i("uitsources")},{i("uitframeworks")},{i("uitresources")},);buildRules=();dependencies=({i("uitdependency")},);name=RemoteControllerUITests;productName=RemoteControllerUITests;productReference={i("uitproduct")};productType="com.apple.product-type.bundle.ui-testing";}};',

f'{i("design")} = {{isa=PBXFileReference;lastKnownFileType=sourcecode.swift;path=ConsoleDesign.swift;sourceTree="<group>";}};',
f'{i("designbuild")} = {{isa=PBXBuildFile;fileRef={i("design")};}};',
f'{i("native")} = {{isa=PBXFileReference;lastKnownFileType=sourcecode.swift;path=NativeSession.swift;sourceTree="<group>";}};',
f'{i("nativebuild")} = {{isa=PBXBuildFile;fileRef={i("native")};}};',
f'{i("protocol")} = {{isa=PBXFileReference;lastKnownFileType=sourcecode.swift;path=ControlProtocol.swift;sourceTree="<group>";}};',
f'{i("protocolbuild")} = {{isa=PBXBuildFile;fileRef={i("protocol")};}};',
f'{i("webrtcpkg")} = {{isa=XCRemoteSwiftPackageReference;repositoryURL="https://github.com/stasel/WebRTC.git";requirement={{kind=exactVersion;version=157.0.0;}};}};',
f'{i("webrtcproduct")} = {{isa=XCSwiftPackageProductDependency;package={i("webrtcpkg")};productName=WebRTC;}};',
f'{i("webrtcbuild")} = {{isa=PBXBuildFile;productRef={i("webrtcproduct")};}};',
f'{i("endpoint")} = {{isa=PBXFileReference;lastKnownFileType=sourcecode.swift;path=Endpoint.swift;sourceTree="<group>";}};',
f'{i("endpointbuild")} = {{isa=PBXBuildFile;fileRef={i("endpoint")};}};',
f'{i("file")} = {{isa=PBXFileReference;lastKnownFileType=sourcecode.swift;path=RemoteController.swift;sourceTree="<group>";}};',
f'{i("info")} = {{isa=PBXFileReference;lastKnownFileType=text.plist.xml;path=Info.plist;sourceTree="<group>";}};',
f'{i("product")} = {{isa=PBXFileReference;explicitFileType=wrapper.application;path=RemoteController.app;sourceTree=BUILT_PRODUCTS_DIR;}};',
f'{i("buildfile")} = {{isa=PBXBuildFile;fileRef={i("file")};}};',
f'{i("root")} = {{isa=PBXGroup;children=({i("assets")},{i("uitfile")},{i("file")},{i("design")},{i("endpoint")},{i("native")},{i("trust")},{i("audioh")},{i("audiom")},{i("bridge")},{i("protocol")},{i("info")},{i("products")},);sourceTree="<group>";}};',
f'{i("products")} = {{isa=PBXGroup;children=({i("product")},{i("uitproduct")},);name=Products;sourceTree="<group>";}};',
f'{i("sources")} = {{isa=PBXSourcesBuildPhase;buildActionMask=2147483647;files=({i("buildfile")},{i("endpointbuild")},{i("nativebuild")},{i("trustbuild")},{i("audiobuild")},{i("protocolbuild")},{i("designbuild")},);runOnlyForDeploymentPostprocessing=0;}};',
f'{i("frameworks")} = {{isa=PBXFrameworksBuildPhase;buildActionMask=2147483647;files=({i("webrtcbuild")},);runOnlyForDeploymentPostprocessing=0;}};',
f'{i("resources")} = {{isa=PBXResourcesBuildPhase;buildActionMask=2147483647;files=({i("assetsbuild")},);runOnlyForDeploymentPostprocessing=0;}};',
f'{i("target")} = {{isa=PBXNativeTarget;buildConfigurationList={i("targetconfigs")};buildPhases=({i("sources")},{i("frameworks")},{i("resources")},);buildRules=();dependencies=();packageProductDependencies=({i("webrtcproduct")},);name=RemoteController;productName=RemoteController;productReference={i("product")};productType="com.apple.product-type.application";}};',
f'{i("project")} = {{isa=PBXProject;attributes={{LastUpgradeCheck=1600;}};buildConfigurationList={i("projectconfigs")};compatibilityVersion="Xcode 14.0";developmentRegion=en;hasScannedForEncodings=0;knownRegions=(en,Base,"zh-HK",);mainGroup={i("root")};productRefGroup={i("products")};projectDirPath="";projectRoot="";packageReferences=({i("webrtcpkg")},);targets=({i("target")},{i("uitarget")},);}};',
]
for label,config,settings in [
('pd','Debug','SDKROOT=iphoneos;IPHONEOS_DEPLOYMENT_TARGET=15.0;SWIFT_VERSION=5.0;SWIFT_OPTIMIZATION_LEVEL="-Onone";'),
('pr','Release','SDKROOT=iphoneos;IPHONEOS_DEPLOYMENT_TARGET=15.0;SWIFT_VERSION=5.0;SWIFT_OPTIMIZATION_LEVEL="-O";'),
('td','Debug','ASSETCATALOG_COMPILER_APPICON_NAME=AppIcon;PRODUCT_BUNDLE_IDENTIFIER=__CONTROLLER_BUNDLE_ID__;PRODUCT_NAME="$(TARGET_NAME)";INFOPLIST_FILE=Info.plist;CLANG_ENABLE_OBJC_ARC=YES;SWIFT_OBJC_BRIDGING_HEADER="RemoteController-Bridging-Header.h";TARGETED_DEVICE_FAMILY="1,2";CODE_SIGN_STYLE=Automatic;SWIFT_EMIT_LOC_STRINGS=YES;LD_RUNPATH_SEARCH_PATHS="$(inherited) @executable_path/Frameworks";'),
('tr','Release','ASSETCATALOG_COMPILER_APPICON_NAME=AppIcon;PRODUCT_BUNDLE_IDENTIFIER=__CONTROLLER_BUNDLE_ID__;PRODUCT_NAME="$(TARGET_NAME)";INFOPLIST_FILE=Info.plist;CLANG_ENABLE_OBJC_ARC=YES;SWIFT_OBJC_BRIDGING_HEADER="RemoteController-Bridging-Header.h";TARGETED_DEVICE_FAMILY="1,2";CODE_SIGN_STYLE=Automatic;SWIFT_EMIT_LOC_STRINGS=YES;LD_RUNPATH_SEARCH_PATHS="$(inherited) @executable_path/Frameworks";')]:
 settings=settings.replace('__CONTROLLER_BUNDLE_ID__',controller_bundle_id)
 objects.append(f'{i(label)} = {{isa=XCBuildConfiguration;buildSettings={{{settings}}};name={config};}};')
for label,config in [('uitdebug','Debug'),('uitrelease','Release')]:
 settings='GENERATE_INFOPLIST_FILE=YES;PRODUCT_BUNDLE_IDENTIFIER=__CONTROLLER_BUNDLE_ID__.uitests;PRODUCT_NAME="$(TARGET_NAME)";TARGETED_DEVICE_FAMILY="1,2";TEST_TARGET_NAME=RemoteController;CODE_SIGN_STYLE=Automatic;SWIFT_VERSION=5.0;IPHONEOS_DEPLOYMENT_TARGET=15.0;ENABLE_TESTING_SEARCH_PATHS=YES;ALWAYS_EMBED_SWIFT_STANDARD_LIBRARIES=YES;LD_RUNPATH_SEARCH_PATHS="$(inherited) @executable_path/Frameworks @loader_path/Frameworks";'
 settings=settings.replace('__CONTROLLER_BUNDLE_ID__',controller_bundle_id)
 objects.append(f'{i(label)} = {{isa=XCBuildConfiguration;buildSettings={{{settings}}};name={config};}};')
for label,children in [('projectconfigs',['pd','pr']),('targetconfigs',['td','tr']),('uitconfigs',['uitdebug','uitrelease'])]:
 objects.append(f'{i(label)} = {{isa=XCConfigurationList;buildConfigurations=({",".join(i(c) for c in children)},);defaultConfigurationIsVisible=0;defaultConfigurationName=Release;}};')
(project/'project.pbxproj').write_text('// !$*UTF8*$!\n{archiveVersion=1;classes={};objectVersion=56;objects={\n'+'\n'.join(objects)+f'\n}};rootObject={i("project")};}}\n',encoding='utf-8')
schemes=project/'xcshareddata/xcschemes';schemes.mkdir(parents=True,exist_ok=True)
(schemes/'RemoteController.xcscheme').write_text(f'''<?xml version="1.0" encoding="UTF-8"?><Scheme LastUpgradeVersion="1600" version="1.3"><BuildAction parallelizeBuildables="YES" buildImplicitDependencies="YES"><BuildActionEntries><BuildActionEntry buildForTesting="YES" buildForRunning="YES" buildForProfiling="YES" buildForArchiving="YES" buildForAnalyzing="YES"><BuildableReference BuildableIdentifier="primary" BlueprintIdentifier="{i('target')}" BuildableName="RemoteController.app" BlueprintName="RemoteController" ReferencedContainer="container:RemoteController.xcodeproj"/></BuildActionEntry><BuildActionEntry buildForTesting="YES" buildForRunning="NO" buildForProfiling="NO" buildForArchiving="NO" buildForAnalyzing="YES"><BuildableReference BuildableIdentifier="primary" BlueprintIdentifier="{i('uitarget')}" BuildableName="RemoteControllerUITests.xctest" BlueprintName="RemoteControllerUITests" ReferencedContainer="container:RemoteController.xcodeproj"/></BuildActionEntry></BuildActionEntries></BuildAction><TestAction buildConfiguration="Debug" selectedDebuggerIdentifier="Xcode.DebuggerFoundation.Debugger.LLDB" selectedLauncherIdentifier="Xcode.IDEFoundation.Launcher.LLDB" shouldUseLaunchSchemeArgsEnv="YES"><Testables><TestableReference skipped="NO"><BuildableReference BuildableIdentifier="primary" BlueprintIdentifier="{i('uitarget')}" BuildableName="RemoteControllerUITests.xctest" BlueprintName="RemoteControllerUITests" ReferencedContainer="container:RemoteController.xcodeproj"/></TestableReference></Testables></TestAction><LaunchAction buildConfiguration="Debug"/><ProfileAction buildConfiguration="Release"/><AnalyzeAction buildConfiguration="Debug"/><ArchiveAction buildConfiguration="Release" revealArchiveInOrganizer="YES"/></Scheme>''',encoding='utf-8')
print(project)
