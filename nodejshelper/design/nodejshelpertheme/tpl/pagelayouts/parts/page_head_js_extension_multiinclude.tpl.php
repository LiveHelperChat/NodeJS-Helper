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
            'instance_id':<?php if (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) : ?><?php echo erLhcoreClassInstance::getInstance()->id?><?php else : ?>0<?php endif; ?>
        };
        confLH.defaut_chat_message_sinterval = confLH.chat_message_sinterval;
        <?php if (erLhcoreClassSystem::instance()->SiteAccess == 'site_admin' && erLhcoreClassUser::instance()->isLogged()) :
        $currentUser = erLhcoreClassUser::instance();
        $userData = $currentUser->getUserData(true); ?>
        lh.nodejsHelperOptions.typer = typeof lh.nodejsHelperOptions.typer !== 'undefined' ? lh.nodejsHelperOptions.typer : '<?php echo htmlspecialchars($userData->name_support,ENT_QUOTES);?> <?php echo erTranslationClassLhTranslation::getInstance()->getTranslation('chat/chat','is typing now...')?>';
        <?php endif;?>
    </script>
    <script src="<?php echo erLhcoreClassDesign::designJS('js/nodejshelper.admin.min.js');?>"></script>
<?php endif; ?>
