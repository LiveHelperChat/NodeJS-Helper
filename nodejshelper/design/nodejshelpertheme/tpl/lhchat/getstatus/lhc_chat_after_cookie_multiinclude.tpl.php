lh_inst.nodejsHelperOptions = {
    'hostname':'<?php echo erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('hostname')?>',
    'path':'<?php echo erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('path')?>',
    'port':'<?php echo erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('port')?>',
    'secure':'<?php echo erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('secure')?>',
};

var thnjs = document.getElementsByTagName('head')[0];
var snjs = document.createElement('script');
snjs.setAttribute('async',true);
snjs.setAttribute('type','text/javascript');
snjs.setAttribute('src','<?php echo erLhcoreClassModelChatConfig::fetch('explicit_http_mode')->current_value?>//<?php echo $_SERVER['HTTP_HOST']?><?php echo erLhcoreClassDesign::designJS('js/nodejshelper.min.js');?>');
thnjs.appendChild(snjs);