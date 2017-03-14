<?php include(erLhcoreClassDesign::designtpl('pagelayouts/parts/nodejshelper_config.tpl.php'));?>
<?php if ( (isset($Result['is_sync_required']) && $Result['is_sync_required'] === true) || erLhcoreClassSystem::instance()->SiteAccess == 'site_admin' ) : ?>
<script>
<?php include(erLhcoreClassDesign::designtpl('pagelayouts/parts/nodejshelper_config_variable.tpl.php'));?>
<?php if (erLhcoreClassSystem::instance()->SiteAccess == 'site_admin' && erLhcoreClassUser::instance()->isLogged()) : 
$currentUser = erLhcoreClassUser::instance();
$userData = $currentUser->getUserData(true); ?>
nodejshelperConfig.typer = typeof nodejshelperConfig.typer !== 'undefined' ? nodejshelperConfig.typer : '<?php echo htmlspecialchars($userData->name_support,ENT_QUOTES);?> <?php echo erTranslationClassLhTranslation::getInstance()->getTranslation('chat/chat','is typing now...')?>';
<?php endif;?>
</script>
<?php if ($nodeJsHelperSettings['use_cdn'] == true) : ?>
<script src="https://cdnjs.cloudflare.com/ajax/libs/socket.io/1.7.3/socket.io.min.js"></script>
<?php else : ?>
<script type="text/javascript" language="javascript" src="<?php echo erLhcoreClassDesign::designJS('js/socket.io.1.7.3.min.js');?>"></script>
<?php endif;?>
<script type="text/javascript" language="javascript" src="<?php echo erLhcoreClassDesign::designJS('js/customjs.js');?>"></script>
<?php endif; ?>