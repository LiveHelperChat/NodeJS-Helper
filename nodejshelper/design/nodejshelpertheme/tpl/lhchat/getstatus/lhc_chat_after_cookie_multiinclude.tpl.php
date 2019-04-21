<?php if (
        strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 10.0') === false &&
        strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 9.0') === false &&
        strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 8.0') === false &&
        strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 7.0') === false
) : ?>

lh_inst.nodejsHelperOptions = {
    'hostname':'<?php echo erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('hostname')?>',
    'path':'<?php echo erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('path')?>',
    'port':'<?php echo erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('port')?>',
    'secure':'<?php echo erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('secure')?>',
    'hash': '<?php 
            $date = time();
            echo sha1($date . 'Custom' . erConfigClassLhConfig::getInstance()->getSetting('site','secrethash')) . '.' . $date;
            ?>',
        <?php if (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) { ?>    
        'instance_id':'<?php echo erLhcoreClassInstance::getInstance()->id?>',
        <?php } else { ?>
        'instance_id':'0',
        <?php } ?>
};

var thnjs = document.getElementsByTagName('head')[0];
var snjs = document.createElement('script');
snjs.setAttribute('async',true);
snjs.setAttribute('type','text/javascript');
snjs.setAttribute('src','<?php echo erLhcoreClassModelChatConfig::fetch('explicit_http_mode')->current_value?>//<?php echo $_SERVER['HTTP_HOST']?><?php echo erLhcoreClassDesign::designJS('js/nodejshelper.min.js');?>');
thnjs.appendChild(snjs);
<?php endif; ?>