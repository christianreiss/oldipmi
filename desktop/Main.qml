import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import org.kde.kirigami as Kirigami
import OldIPMI 1.0

Kirigami.ApplicationWindow {
 id: window
 width: 1250; height: 800; minimumWidth: 850; minimumHeight: 550
 title: "OldIPMI"
 visible: true
 property string hostId: ""
 property string certPath: ""
 property string keyPath: ""
 property string pendingPower: ""
 Component.onCompleted: { if (controller.hosts.length) window.loadHost(controller.hosts[0]) }
 property bool busy: controller.state !== "disconnected"
 function profile() { return {id:hostId,name:hostName.text,host:address.text.trim(),username:username.text,family:["ami","aten","avocent","cisco"][family.currentIndex],http:plainHttp.checked,legacyTLS:legacyTls.checked,allowPlainConsole:plainConsole.checked,remember:remember.checked,jnlpPath:jnlpPath.text,clientCert:certPath,clientKey:keyPath} }
 function loadHost(p) {hostId=p.id||"";hostName.text=p.name||"";address.text=p.host||"";username.text=p.username||"";family.currentIndex=Math.max(0,["ami","aten","avocent","cisco"].indexOf(p.family));plainHttp.checked=!!p.http;legacyTls.checked=!!p.legacyTLS;plainConsole.checked=!!p.allowPlainConsole;remember.checked=p.remember===undefined?true:!!p.remember;jnlpPath.text=p.jnlpPath||"";certPath=p.clientCert||"";keyPath=p.clientKey||"";password.text=""}
 header: ToolBar {
  RowLayout {
   anchors.fill: parent; anchors.margins: 6
   Kirigami.Heading {text:"OldIPMI";level:2;Layout.leftMargin:8}
   Label {text:({disconnected:"Disconnected",logging_in:"Logging in",discovering:"Discovering console",connecting:"Connecting",connected:"Connected"})[controller.state]||controller.state; color:controller.state==="connected"?Kirigami.Theme.positiveTextColor:Kirigami.Theme.textColor}
   Item {Layout.fillWidth:true}
   ToolButton {objectName:"mcpButton";text:"MCP";icon.name:"network-connect";ToolTip.visible:hovered;ToolTip.text:"Instructions for AI agents (MCP)";onClicked:mcpDialog.open()}
   ToolButton {text:viewport.fit?"Actual size":"Fit to window";onClicked:viewport.fit=!viewport.fit}
   ToolButton {text:window.visibility===Window.FullScreen?"Leave fullscreen":"Fullscreen";onClicked:window.visibility=window.visibility===Window.FullScreen?Window.Windowed:Window.FullScreen}
   BusyIndicator {running:window.busy&&controller.state!=="connected";visible:running;Layout.preferredWidth:24;Layout.preferredHeight:24}
   ToolButton {text:controller.state==="connected"?"Disconnect":"Cancel";enabled:window.busy;onClicked:controller.disconnectHost()}
  }
 }
 RowLayout {
  anchors.fill: parent;spacing:0
  ScrollView {
   id:sidebar;Layout.preferredWidth:300;Layout.fillHeight:true;clip:true;visible:window.visibility!==Window.FullScreen
   ColumnLayout {
    width:sidebar.availableWidth;spacing:10
    Kirigami.Heading {text:"Profiles";level:3;Layout.margins:12}
    Repeater {
     model:controller.hosts
     delegate:RowLayout {
      required property var modelData
      Layout.fillWidth:true;spacing:0
      ItemDelegate {Layout.fillWidth:true;text:modelData.name||modelData.host;highlighted:window.hostId===modelData.id;enabled:!window.busy;onClicked:window.loadHost(modelData)}
      ToolButton {text:"Open Console";icon.name:"media-playback-start";display:AbstractButton.IconOnly;enabled:!window.busy
       ToolTip.visible:hovered;ToolTip.text:"Open "+(modelData.name||modelData.host)+" console"
       Accessible.name:ToolTip.text
       onClicked:{window.loadHost(modelData);controller.connectHost(modelData,"")}
      }
     }
    }
    RowLayout {Layout.margins:12
     Button {text:"New";enabled:!window.busy;onClicked:window.loadHost({})}
     Button {text:"Remove";enabled:!window.busy&&window.hostId!=="";onClicked:removeDialog.open()}
    }
    Kirigami.Separator {Layout.fillWidth:true}
    ColumnLayout {
     Layout.margins:12;Layout.fillWidth:true;enabled:!window.busy
     Label {text:"Name"}
     TextField {id:hostName;Layout.fillWidth:true;placeholderText:"Server console"}
     Label {text:"BMC address"}
     TextField {id:address;Layout.fillWidth:true;placeholderText:"bmc.example.net"}
     Label {text:"Console family"}
     ComboBox {id:family;Layout.fillWidth:true;model:["AMI MegaRAC","ATEN / Supermicro","Avocent / Dell iDRAC","Cisco IMC (Avocent)"]}
     Label {text:"Username"}
     TextField {id:username;Layout.fillWidth:true}
     Label {text:"Password"}
     TextField {id:password;Layout.fillWidth:true;echoMode:TextInput.Password;placeholderText:remember.checked?"Use saved KWallet password":"Session only";onAccepted:{controller.connectHost(window.profile(),text);text=""}}
     CheckBox {id:remember;text:"Save password in KWallet"}
     Button {text:"Save profile";Layout.fillWidth:true;onClicked:{let p=controller.saveHost(window.profile(),password.text,remember.checked);if(p.id){window.hostId=p.id;password.text=""}}}
     Button {text:"Open Console";highlighted:true;Layout.fillWidth:true;enabled:address.text.trim()!=="";onClicked:{controller.connectHost(window.profile(),password.text);password.text=""}}
     Button {text:"Open fresh JNLP…";Layout.fillWidth:true;enabled:address.text.trim()!==""&&family.currentIndex<2;onClicked:controller.openJNLP(window.profile())}
     Label {text:"Compatibility";font.bold:true;Layout.topMargin:8}
     CheckBox {id:plainHttp;text:"HTTP login (unencrypted)"}
     CheckBox {id:legacyTls;text:"Allow legacy / unverified TLS"}
     CheckBox {id:plainConsole;text:"Allow unencrypted console / media"}
     Label {text:"Exceptions apply only to this host.";font.pointSize:9;wrapMode:Text.Wrap;Layout.fillWidth:true}
     Label {text:"JNLP endpoint override"}
     TextField {id:jnlpPath;Layout.fillWidth:true;placeholderText:"Automatic"}
     Button {text:window.certPath?"Client certificate selected":"Client certificate…";Layout.fillWidth:true;onClicked:window.certPath=controller.chooseCertificate(false)}
     Button {text:window.keyPath?"Client key selected":"Client key…";Layout.fillWidth:true;onClicked:window.keyPath=controller.chooseCertificate(true)}
    }
   }
  }
  Kirigami.Separator {Layout.fillHeight:true;visible:sidebar.visible}
  ColumnLayout {
   Layout.fillWidth:true;Layout.fillHeight:true;spacing:0
   Pane {Layout.fillWidth:true;visible:!!controller.consoleInfo.protocol
    ColumnLayout {anchors.fill:parent
     RowLayout {Layout.fillWidth:true
      Label {text:controller.consoleInfo.protocol||"";font.bold:true}
      Label {text:controller.consoleInfo.resolution||""}
      Item {Layout.fillWidth:true}
      ToolButton {id:details;objectName:"consoleDetails";checkable:true;text:checked?"Hide console info":"Console info"}
     }
     ColumnLayout {visible:details.checked;Layout.fillWidth:true
      Label {text:controller.state==="disconnected"?"Most recent connection":"Current connection";font.italic:true}
      Label {visible:!!controller.consoleInfo.protocolVersion;text:"Protocol version: "+(controller.consoleInfo.protocolVersion||"")}
      Repeater {model:controller.consoleInfo.endpoints||[]
       delegate:Label {required property var modelData;Layout.fillWidth:true;wrapMode:Text.Wrap;text:modelData.role+": "+modelData.address+" · "+modelData.transport}
      }
      Label {text:"Console features: "+(controller.consoleInfo.capabilities||[]).join(", ");Layout.fillWidth:true;wrapMode:Text.Wrap}
      Label {visible:!!controller.consoleInfo.legacyTLS;text:"Legacy TLS enabled; certificate verification disabled for this profile.";Layout.fillWidth:true;wrapMode:Text.Wrap}
     }
    }
   }
   ConsoleView {id:viewport;objectName:"console";Layout.fillWidth:true;Layout.fillHeight:true}
   ToolBar {Layout.fillWidth:true
    RowLayout {
     anchors.fill:parent;anchors.margins:5
     ToolButton {text:controller.mounted?"Eject ISO":"Mount ISO…";enabled:controller.state==="connected"&&family.currentIndex<2;onClicked:controller.mounted?controller.ejectISO():controller.mountISO()}
     ToolButton {text:"Ctrl+Alt+Del";enabled:controller.state==="connected";onClicked:{controller.releaseInput();controller.key(224,true);controller.key(226,true);controller.key(76,true);controller.key(76,false);controller.key(226,false);controller.key(224,false)}}
     Item {Layout.fillWidth:true}
     ToolButton {text:"Power";enabled:controller.state==="connected";onClicked:powerMenu.open()
      Menu {id:powerMenu
       MenuItem {text:"Power on";onTriggered:{pendingPower="on";powerDialog.open()}}
       MenuItem {text:"Graceful shutdown";onTriggered:{pendingPower="soft";powerDialog.open()}}
       MenuItem {text:"Reset";onTriggered:{pendingPower="reset";powerDialog.open()}}
       MenuItem {text:"Power off";onTriggered:{pendingPower="off";powerDialog.open()}}
      }
     }
    }
   }
   Label {text:controller.message;wrapMode:Text.Wrap;Layout.fillWidth:true;Layout.margins:10}
  }
 }
 Dialog {id:powerDialog;title:"Confirm power operation";anchors.centerIn:parent;modal:true;standardButtons:Dialog.Ok|Dialog.Cancel
  Label {text:"Send ‘"+window.pendingPower+"’ to "+address.text+"?\nThis may interrupt the host operating system."}
  onAccepted:controller.power(window.pendingPower)
 }
 Dialog {id:mcpDialog;objectName:"mcpDialog";title:"MCP — instructions for AI agents";anchors.centerIn:parent;modal:true;width:Math.min(window.width-40,760);height:Math.min(window.height-40,640);standardButtons:Dialog.Close
  ColumnLayout {
   anchors.fill:parent
   Label {text:agent.listening?"Agent socket is active (local only, no authentication): "+agent.socketPath:"Agent socket is NOT active (another OldIPMI owns it, or --no-agent-api)."
    color:agent.listening?Kirigami.Theme.positiveTextColor:Kirigami.Theme.negativeTextColor;wrapMode:Text.Wrap;Layout.fillWidth:true}
   ScrollView {Layout.fillWidth:true;Layout.fillHeight:true;clip:true
    TextArea {id:mcpText;objectName:"mcpText";readOnly:true;wrapMode:TextEdit.Wrap;selectByMouse:true;font.family:"monospace";text:agent.mcpInstructions()}
   }
   RowLayout {
    Button {text:"Copy instructions";icon.name:"edit-copy";onClicked:{mcpText.selectAll();mcpText.copy();mcpText.deselect()}}
    Button {text:"Copy register command";onClicked:{mcpCmd.text="claude mcp add oldipmi -- "+agent.mcpPath;mcpCmd.selectAll();mcpCmd.copy()}}
    TextEdit {id:mcpCmd;visible:false}
   }
  }
 }
 Dialog {id:removeDialog;title:"Remove profile?";anchors.centerIn:parent;modal:true;standardButtons:Dialog.Ok|Dialog.Cancel
  Label {text:"Remove this profile and its saved KWallet password?"}
  onAccepted:{if(controller.removeHost(window.hostId))window.loadHost({})}
 }
 Connections {
  target:controller
  function onProfileActivated(profile){window.loadHost(profile)}
  function onPasswordRequired(){credentialsPassword.text="";credentialsRemember.checked=remember.checked;credentialsDialog.open()}
  function onWalletUnavailable(){walletDialog.open()}
 }
 Dialog {id:credentialsDialog;title:"Open "+(hostName.text||address.text)+" console";anchors.centerIn:parent;modal:true;standardButtons:Dialog.Ok|Dialog.Cancel
  ColumnLayout {
   Label {text:"Password for "+username.text+" at "+address.text;wrapMode:Text.Wrap;Layout.maximumWidth:450}
   TextField {id:credentialsPassword;Layout.fillWidth:true;echoMode:TextInput.Password;onAccepted:credentialsDialog.accept()}
   CheckBox {id:credentialsRemember;text:"Remember in KWallet";checked:true}
  }
  onOpened:credentialsPassword.forceActiveFocus()
  onAccepted:{remember.checked=credentialsRemember.checked;controller.connectHost(window.profile(),credentialsPassword.text);credentialsPassword.text=""}
  onRejected:{credentialsPassword.text="";controller.cancelCredentials()}
 }
 Dialog {id:walletDialog;title:"Profile or password could not be saved";anchors.centerIn:parent;modal:true;standardButtons:Dialog.Cancel
  ColumnLayout {
   Label {text:controller.message+"\nYou can still open this console for this session.";wrapMode:Text.Wrap;Layout.maximumWidth:450}
   Button {text:"Use for this session";onClicked:{walletDialog.close();controller.continueWithoutWallet()}}
  }
  onRejected:controller.cancelCredentials()
 }

}
