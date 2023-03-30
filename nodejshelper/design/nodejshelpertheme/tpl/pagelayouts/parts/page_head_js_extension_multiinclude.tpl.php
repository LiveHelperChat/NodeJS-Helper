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
            'track_visitors':<?php echo erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('track_visitors')?>,
            'instance_id':<?php if (erLhcoreClassModule::getExtensionInstance('erLhcoreClassExtensionNodejshelper')->getSettingVariable('automated_hosting')) : ?><?php echo erLhcoreClassInstance::getInstance()->id?><?php else : ?>0<?php endif; ?>
        };
        confLH.defaut_chat_message_sinterval = confLH.chat_message_sinterval;
        <?php if (erLhcoreClassSystem::instance()->SiteAccess == 'site_admin' && erLhcoreClassUser::instance()->isLogged()) :
        $currentUser = erLhcoreClassUser::instance();
        $userData = $currentUser->getUserData(true); ?>
        lh.nodejsHelperOptions.typer_ending_txt = <?php echo json_encode(htmlspecialchars_decode(erTranslationClassLhTranslation::getInstance()->getTranslation('chat/chat','is typing now...')));?>;
        lh.nodejsHelperOptions.typer = typeof lh.nodejsHelperOptions.typer !== 'undefined' ? lh.nodejsHelperOptions.typer : '<?php echo htmlspecialchars($userData->name_support,ENT_QUOTES);?> ' + lh.nodejsHelperOptions.typer_ending_txt;
        lh.nodejsHelperOptions.trans = <?php echo json_encode(array(
            'online' => erTranslationClassLhTranslation::getInstance()->getTranslation('chat/adminchat','Visitor online'),
            'offline' => erTranslationClassLhTranslation::getInstance()->getTranslation('chat/adminchat','Visitor offline')
        )); ?>;
        <?php endif;?>
    </script>
    <script src="<?php echo erLhcoreClassDesign::designJS('js/nodejshelper.admin.min.js');?>"></script>
<?php endif; ?>
