<?php if (isset($Result['is_sync_required']) && $Result['is_sync_required'] === true) : ?>

<?php if (
strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 10.0') === false &&
strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 9.0') === false &&
strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 8.0') === false &&
strpos($_SERVER['HTTP_USER_AGENT'],'MSIE 7.0') === false
) : ?>

<script>
lh.nodejsHelperOptions = {
    'hostname':'<?php echo erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('hostname')?>',
    'path':'<?php echo erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('path')?>',
    'port':'<?php echo erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('port')?>',
    'secure':'<?php echo erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('secure')?>',
    'instance_id':<?php if (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) : ?><?php echo erLhcoreClassInstance::getInstance()->id?><?php else : ?>0<?php endif; ?>,
    'hide_typing': <?php echo json_encode(erLhcoreClassModelChatConfig::fetch('ignore_typing')->current_value == 1);?>,
    'typing_text' : <?php echo json_encode(htmlspecialchars_decode(erTranslationClassLhTranslation::getInstance()->getTranslation('chat/startchat','Visitor is typing')));?>
};
confLH.defaut_chat_message_sinterval = confLH.chat_message_sinterval;
</script>
<script src="<?php echo erLhcoreClassDesign::designJS('js/nodejshelper.widget.min.js');?>"></script>
<?php endif; ?>
<?php endif; ?>
