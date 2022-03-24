import Ember from 'ember';

export default Ember.Route.extend({
  auth: Ember.inject.service('auth'),
  controller: Ember.inject.controller('login'),
  application: Ember.inject.controller('application'),

  // beforeModel() {
  //   this.get('auth').clearIdToken();
  // },

  actions: {
    authenticate() {
      const { login, password } = this.get('controller').getProperties('login', 'password');
      this.get('auth').login(login, password).then(() => {
        this.transitionTo('/');
      }, (err) => {
        alert('Login Error: ' + err);
      });
    }
  }
});
